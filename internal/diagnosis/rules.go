package diagnosis

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Thresholds of the rules (docs/diagnosis.md).
const (
	// busyTopic is the messages a second below which a topic is too quiet
	// for its consumers' pace to be told apart from its own.
	busyTopic = 1.0
	// slowShare is the share of a topic's messages a group must consume to
	// keep up; below it, it falls behind.
	slowShare = 0.9
	// behindFloor is the lag below which a group counts as keeping up.
	behindFloor = 60.0
	// retentionShare is how much of a topic's retention a group may be
	// behind before messages risk being deleted unread.
	retentionShare = 0.5
	// rebalancesPerHour is how often a group's members may change in an
	// hour before its rebalances cost it.
	rebalancesPerHour = 6
	// hotShare is how many times its fair share of a topic's data one
	// partition holds before it counts as hot, and hotFloor the size below
	// which skew does not matter yet.
	hotShare = 2.0
	hotFloor = 64e6
	// errorShare and retryShare are the shares of a producer's records that
	// may fail, or be sent again, before it is worth looking at.
	errorShare = 0.01
	retryShare = 0.1
	// quietProducer is the records a second below which a producer's
	// shares are too noisy to judge.
	quietProducer = 0.1
	// unreportedFloor and unreportedShare are the bytes a second, and the
	// share of a topic's, that may come from producers without telemetry
	// before they are worth mentioning: the topology map's thresholds.
	unreportedFloor = 100.0
	unreportedShare = 0.1
)

// consumerTooSlow finds groups that consume less of a topic than it
// receives, so that they fall further behind.
func consumerTooSlow(s Snapshot) []Finding {
	var found []Finding
	for _, g := range s.Groups {
		// Nobody consuming is its own diagnosis, and while a group shares
		// its partitions out, its members read nothing for a moment.
		if g.Members == 0 || g.State != "Stable" {
			continue
		}
		for _, r := range g.Reads {
			t, ok := s.topic(r.Topic)
			// A group none of whose members read the topic any more has
			// old offsets on it, not a slow consumer; one that went back to
			// replay it reads as consuming nothing for a while.
			if !ok || r.AssignedMembers == 0 || r.OffsetsReset || t.MessagesInPerSecond < busyTopic ||
				r.ConsumedPerSecond >= slowShare*t.MessagesInPerSecond || r.LagSeconds < behindFloor || !r.growing() {
				continue
			}
			earlier := "behind as much"
			if r.LagSecondsEarlier != nil && !r.Capped {
				earlier = behind(*r.LagSecondsEarlier)
			}
			f := Finding{
				Severity: Warning,
				Subject:  Subject{ConsumerGroup: g.Name, Topic: r.Topic},
				Title:    fmt.Sprintf("%s can't keep up with %s", g.Name, r.Topic),
				Explanation: fmt.Sprintf(
					"%s consumes %s messages a second of %s, which receives %s, so it falls further behind: %s now, %s %s ago.",
					g.Name, rate(r.ConsumedPerSecond), r.Topic, rate(t.MessagesInPerSecond),
					duration(r.LagSeconds), earlier, duration(s.Window.Seconds())),
			}
			if spare := t.Partitions - r.AssignedMembers; spare > 0 {
				f.Suggestion = fmt.Sprintf(
					"Run more consumers in %s. It has %s for the %s of %s, so up to %d more can share the work.",
					g.Name, plural(r.AssignedMembers, "consumer"), plural(t.Partitions, "partition"), r.Topic, spare)
			} else {
				f.Suggestion = fmt.Sprintf(
					"%s already has a consumer for each of the %s of %s. Make each one faster, for example by processing messages in batches, or add partitions to %s so that more consumers can share it. Adding partitions sends keys to other partitions, so for a while messages with the same key may arrive out of order.",
					g.Name, plural(t.Partitions, "partition"), r.Topic, r.Topic)
			}
			found = append(found, f)
		}
	}
	return found
}

// nobodyConsuming finds groups without members whose topics still receive
// messages, so that they fall further behind.
func nobodyConsuming(s Snapshot) []Finding {
	var found []Finding
	for _, g := range s.Groups {
		if g.Members > 0 {
			continue
		}
		for _, r := range g.Reads {
			// Without consumers, messages that keep arriving pile up.
			t, ok := s.topic(r.Topic)
			if !ok || t.MessagesInPerSecond <= 0 || r.LagMessages == 0 {
				continue
			}
			found = append(found, Finding{
				Severity: Warning,
				Subject:  Subject{ConsumerGroup: g.Name, Topic: r.Topic},
				Title:    fmt.Sprintf("Nothing in %s is consuming %s", g.Name, r.Topic),
				Explanation: fmt.Sprintf(
					"%s has no consumers running, while %s receives %s messages a second: it is %s behind, and falls further behind.",
					g.Name, r.Topic, rate(t.MessagesInPerSecond), duration(r.LagSeconds)),
				Suggestion: fmt.Sprintf(
					"Start the consumers of %s again. If nothing uses %s any more, delete it, for example with `kafka-consumer-groups --delete --group %s`, so that it no longer counts as behind.",
					g.Name, g.Name, g.Name),
			})
		}
	}
	return found
}

// behindRetention finds groups so far behind that retention deletes, or
// deleted, messages before they read them.
func behindRetention(s Snapshot) []Finding {
	var found []Finding
	for _, g := range s.Groups {
		for _, r := range g.Reads {
			t, ok := s.topic(r.Topic)
			if !ok || t.Compacted || t.RetentionMs == nil {
				continue
			}
			retention := float64(*t.RetentionMs) / 1000
			subject := Subject{ConsumerGroup: g.Name, Topic: r.Topic}
			switch {
			case r.LostPartitions > 0:
				where := fmt.Sprintf(", on %d of its %s", r.LostPartitions, plural(t.Partitions, "partition"))
				if r.LostPartitions >= t.Partitions {
					where = ""
				}
				found = append(found, Finding{
					Severity: Critical,
					Subject:  subject,
					Title:    fmt.Sprintf("%s lost messages of %s to retention", g.Name, r.Topic),
					Explanation: fmt.Sprintf(
						"%s keeps messages for %s, and deleted messages %s had not read yet%s.",
						r.Topic, duration(retention), g.Name, where),
					Suggestion: fmt.Sprintf(
						"The consumers of %s continue from where `auto.offset.reset` points them: the oldest message kept, or the newest. Check what the missed messages were for, then keep %s from falling that far behind, or keep the messages of %s longer.",
						g.Name, g.Name, r.Topic),
				})
			case r.LagSeconds >= retentionShare*retention && r.LagMessages > 0:
				found = append(found, Finding{
					Severity: Warning,
					Subject:  subject,
					Title:    fmt.Sprintf("%s is close to losing messages of %s", g.Name, r.Topic),
					Explanation: fmt.Sprintf(
						"%s is %s behind on %s, which keeps messages for %s. Unless it catches up, messages are deleted before it reads them.",
						g.Name, duration(r.LagSeconds), r.Topic, duration(retention)),
					Suggestion: fmt.Sprintf(
						"Help %s catch up, or keep the messages of %s longer while it does.", g.Name, r.Topic),
				})
			}
		}
	}
	return found
}

// idleConsumers finds groups with consumers that read nothing.
func idleConsumers(s Snapshot) []Finding {
	var found []Finding
	for _, g := range s.Groups {
		if g.State != "Stable" || g.IdleMembers == 0 || len(g.Reads) == 0 {
			continue
		}
		topics := make([]string, 0, len(g.Reads))
		partitions := 0
		for _, r := range g.Reads {
			topics = append(topics, r.Topic)
			if t, ok := s.topic(r.Topic); ok {
				partitions += t.Partitions
			}
		}
		f := Finding{
			Severity: Info,
			Subject:  Subject{ConsumerGroup: g.Name},
			Title:    g.Name + " has idle consumers",
		}
		if g.Members > partitions {
			f.Explanation = fmt.Sprintf(
				"%s runs %s, but %d of them read nothing: a partition is read by one consumer of a group at a time, and %s %s %s.",
				g.Name, plural(g.Members, "consumer"), g.IdleMembers, list(topics), have(len(topics)), plural(partitions, "partition"))
			f.Suggestion = fmt.Sprintf(
				"Idle consumers take over when another stops, which may be what you want. To have them share the work, add partitions to %s.",
				list(topics))
		} else {
			// Enough partitions, spread unevenly: the range assignor gives
			// each topic's to the first consumers.
			f.Explanation = fmt.Sprintf(
				"%s runs %s, but %d of them read nothing, though %s %s %s to share: they are handed out topic by topic, to the same consumers each time.",
				g.Name, plural(g.Members, "consumer"), g.IdleMembers, list(topics), have(len(topics)), plural(partitions, "partition"))
			f.Suggestion = "Spread them over every consumer with the cooperative-sticky assignor (`partition.assignment.strategy`), or with Kafka's own consumer group protocol (`group.protocol=consumer`, from Kafka 4.0 clients)."
		}
		found = append(found, f)
	}
	return found
}

// frequentRebalances finds groups whose members change so often that they
// keep stopping to share their partitions out again.
func frequentRebalances(s Snapshot) []Finding {
	var found []Finding
	for _, g := range s.Groups {
		if g.Rebalances < rebalancesPerHour {
			continue
		}
		found = append(found, Finding{
			Severity: Warning,
			Subject:  Subject{ConsumerGroup: g.Name},
			Title:    g.Name + " keeps rebalancing",
			Explanation: fmt.Sprintf(
				"The members of %s changed %s in the last hour. Each time, its consumers share the partitions out again, and read less meanwhile.",
				g.Name, plural(g.Rebalances, "time")),
			Suggestion: "Consumers that restart often, or take longer than `max.poll.interval.ms` between polls, cause this. Give each consumer a `group.instance.id`, so that a restart keeps its partitions, and keep the work between polls short, for example with smaller batches (`max.poll.records`).",
		})
	}
	return found
}

// hotPartition finds topics where one partition holds far more than its
// share of the data: a few busy keys.
func hotPartition(s Snapshot) []Finding {
	var found []Finding
	for _, t := range s.Topics {
		n := len(t.PartitionBytes)
		// Only tiered topics keep just their last day on the brokers, where
		// sizes show how messages spread now; partitions added within the
		// day start empty and would make the rest look hot.
		if n < 3 || !t.Tiered || t.PartitionsAdded {
			continue
		}
		total, largest := 0.0, 0.0
		for _, size := range t.PartitionBytes {
			total += size
			largest = max(largest, size)
		}
		if total < hotFloor || largest/total < hotShare/float64(n) {
			continue
		}
		found = append(found, Finding{
			Severity: Warning,
			Subject:  Subject{Topic: t.Name},
			Title:    fmt.Sprintf("One partition of %s takes more than its share", t.Name),
			Explanation: fmt.Sprintf(
				"One of the %s of %s received %s of its messages' bytes over the last day, where each would get %s if keys were spread evenly. Messages with the same key always go to the same partition, so a few busy keys load one partition, and the one consumer reading it, more than the rest.",
				plural(n, "partition"), t.Name, percent(largest/total), percent(1/float64(n))),
			Suggestion: "Pick a key with more distinct values, such as an order ID rather than a country, or leave the key out where the order of messages does not matter.",
		})
	}
	return found
}

// partitionsAdded notes topics that got partitions in the last day while
// consumer groups read them: a key's new messages may go to another
// partition than its earlier ones, so the order of a key's messages may
// have broken for a while.
func partitionsAdded(s Snapshot) []Finding {
	read := map[string]bool{}
	for _, g := range s.Groups {
		for _, r := range g.Reads {
			read[r.Topic] = true
		}
	}
	var found []Finding
	for _, t := range s.Topics {
		if !t.PartitionsAdded || !read[t.Name] {
			continue
		}
		found = append(found, Finding{
			Severity: Info,
			Subject:  Subject{Topic: t.Name},
			Title:    t.Name + " got more partitions",
			Explanation: fmt.Sprintf(
				"%s got partitions in the last day, and has %s now. Messages with the same key always go to the same partition, so since then a key's new messages may go to another partition than its earlier ones, and its consumers may have read them out of order.",
				t.Name, plural(t.Partitions, "partition")),
			Suggestion: fmt.Sprintf(
				"Nothing to do where the order of a key's messages does not matter. Where it does, check what the consumers of %s made of the messages around the change. Consumers find new partitions within 5 minutes (`metadata.max.age.ms`), and those with `auto.offset.reset=latest`, the Java client's default, skip what reached a new partition before they did.",
				t.Name),
		})
	}
	return found
}

// oversizedMessages finds topics that refuse messages larger than they
// accept, and the producers that sent them, as far as they report it.
func oversizedMessages(s Snapshot) []Finding {
	var found []Finding
	for _, t := range s.Topics {
		if t.BytesRejectedPerSecond <= 0 {
			continue
		}
		var from []string
		for _, p := range s.Producers {
			if p.TopicErrorsPerSecond[t.Name] > 0 {
				from = append(from, p.ClientID)
			}
		}
		limit := "the most it accepts"
		if t.MaxMessageBytes > 0 {
			limit = bytes(float64(t.MaxMessageBytes)) + ", the most it accepts"
		}
		by := ""
		if len(from) > 0 {
			by = fmt.Sprintf(" They came from %s.", list(from))
		}
		found = append(found, Finding{
			Severity: Warning,
			Subject:  Subject{Topic: t.Name},
			Title:    t.Name + " refuses messages too large for it",
			Explanation: fmt.Sprintf(
				"Over the last %s, %s refused %s a second of messages larger than %s (`max.message.bytes`).%s Refused messages are not kept: unless their producer handles the error, they are lost.",
				duration(s.Window.Seconds()), t.Name, bytes(t.BytesRejectedPerSecond), limit, by),
			Suggestion: fmt.Sprintf(
				"Keep large payloads, such as files or images, in object storage such as S3, and send a message that refers to them. If messages are this large by nature, compress them in the producer (`compression.type=zstd`), or raise the `max.message.bytes` of %s; the producer's `max.request.size` and the consumers' `max.partition.fetch.bytes` must then allow them too.",
				t.Name),
		})
	}
	return found
}

// producerErrors finds producers whose records fail, or need sending again.
// A record that fails before it is sent, such as one larger than the
// producer's max.request.size or for a topic that does not exist, counts as
// an error but not as a send, so the share is of what the producer tried to
// send. Records a topic refused as too large are oversizedMessages'.
func producerErrors(s Snapshot) []Finding {
	oversized := map[string]bool{}
	for _, t := range s.Topics {
		oversized[t.Name] = t.BytesRejectedPerSecond > 0
	}
	var found []Finding
	for _, p := range s.Producers {
		tried := p.SendsPerSecond + p.ErrorsPerSecond
		if tried < quietProducer {
			continue
		}
		refused, tooLarge := 0.0, 0.0
		var refusedOn []string
		for _, topic := range slices.Sorted(maps.Keys(p.TopicErrorsPerSecond)) {
			errors := p.TopicErrorsPerSecond[topic]
			if errors <= 0 {
				continue
			}
			refused += errors
			if oversized[topic] {
				tooLarge += errors
			} else {
				refusedOn = append(refusedOn, topic)
			}
		}
		errors := max(0, p.ErrorsPerSecond-tooLarge)
		before := max(0, p.ErrorsPerSecond-refused)
		subject := Subject{ClientID: p.ClientID, Principal: p.Principal}
		switch failed, retried := min(1, errors/tried), p.RetriesPerSecond/tried; {
		case failed >= errorShare && before >= errors/2:
			found = append(found, Finding{
				Severity: Warning,
				Subject:  subject,
				Title:    fmt.Sprintf("Records from %s fail before reaching Kafka", p.ClientID),
				Explanation: fmt.Sprintf(
					"%s of the records %s tried to send in the last %s failed in the producer itself, before reaching Kafka: %s a second of %s.",
					percent(failed), p.ClientID, duration(s.Window.Seconds()), rate(errors), rate(tried)),
				Suggestion: fmt.Sprintf(
					"The log of %s says why. The usual causes are records larger than the producer's `max.request.size` (1 MB by default), a topic that does not exist (topics are not created on first use), or %s lacking permission to write to it. Large payloads belong in object storage, with a message that refers to them.",
					p.ClientID, p.Principal),
			})
		case failed >= errorShare:
			on := ""
			if len(refusedOn) > 0 {
				on = ", on " + list(refusedOn)
			}
			found = append(found, Finding{
				Severity: Warning,
				Subject:  subject,
				Title:    "Kafka refuses records from " + p.ClientID,
				Explanation: fmt.Sprintf(
					"Kafka refused %s of the records %s tried to send in the last %s%s: %s a second of %s.",
					percent(failed), p.ClientID, duration(s.Window.Seconds()), on, rate(errors), rate(tried)),
				Suggestion: fmt.Sprintf(
					"The log of %s says why. The usual causes are too few in-sync replicas for `acks=all` while a broker is down, records without a key sent to a compacted topic, and requests that time out (`delivery.timeout.ms`).",
					p.ClientID),
			})
		case retried >= retryShare:
			found = append(found, Finding{
				Severity: Info,
				Subject:  subject,
				Title:    p.ClientID + " sends many records twice",
				Explanation: fmt.Sprintf(
					"%s sent %s of its records again in the last %s. They arrive in the end, but late.",
					p.ClientID, percent(min(1, retried)), duration(s.Window.Seconds())),
				Suggestion: "Retries follow timeouts and brokers that move partitions, such as during a restart. If they go on, check the network between the producer and the cluster, and `request.timeout.ms`.",
			})
		}
	}
	return found
}

// producersWithoutTelemetry finds topics written to by producers that send
// no telemetry, which leaves them off the data flow map.
func producersWithoutTelemetry(s Snapshot) []Finding {
	var found []Finding
	for _, t := range s.Topics {
		// A Postgres source's topics are written by Changeport's own
		// Connect worker.
		if t.Source || t.UnreportedBytesInPerSecond <= unreportedFloor || t.UnreportedBytesInPerSecond <= unreportedShare*t.BytesInPerSecond {
			continue
		}
		found = append(found, Finding{
			Severity: Info,
			Subject:  Subject{Topic: t.Name},
			Title:    fmt.Sprintf("Some producers of %s are unseen", t.Name),
			Explanation: fmt.Sprintf(
				"%s receives %s a second from producers that send no telemetry, so the data flow map cannot show them, nor their errors.",
				t.Name, bytes(t.UnreportedBytesInPerSecond)),
			Suggestion: "Producers on Kafka's Java client 3.7 or newer, or on librdkafka 2.5 or newer, send it by themselves. Older ones work as they are, but stay unseen.",
		})
	}
	return found
}

// have is "has" for one, "have" for more.
func have(n int) string {
	if n == 1 {
		return "has"
	}
	return "have"
}

// list joins names as people do: a, b and c.
func list(names []string) string {
	names = slices.Clone(names)
	slices.Sort(names)
	if len(names) <= 1 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
