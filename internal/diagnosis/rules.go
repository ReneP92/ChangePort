package diagnosis

import (
	"fmt"
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
		if g.Members == 0 {
			// Nobody consuming is its own diagnosis.
			continue
		}
		for _, r := range g.Reads {
			t, ok := s.topic(r.Topic)
			if !ok || t.MessagesInPerSecond < busyTopic || r.ConsumedPerSecond >= slowShare*t.MessagesInPerSecond ||
				r.LagSeconds < behindFloor || !r.growing() {
				continue
			}
			f := Finding{
				Severity: Warning,
				Subject:  Subject{ConsumerGroup: g.Name, Topic: r.Topic},
				Title:    fmt.Sprintf("%s can't keep up with %s", g.Name, r.Topic),
				Explanation: fmt.Sprintf(
					"%s consumes %s messages a second of %s, which receives %s, so it falls further behind: %s now, %s %s ago.",
					g.Name, rate(r.ConsumedPerSecond), r.Topic, rate(t.MessagesInPerSecond),
					duration(r.LagSeconds), duration(*r.LagSecondsEarlier), duration(s.Window.Seconds())),
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
			t, ok := s.topic(r.Topic)
			if !ok || t.MessagesInPerSecond <= 0 || r.LagMessages == 0 || !r.growing() {
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
					"Start the consumers of %s again. If nothing uses %s any more, delete it, for example with kafka-consumer-groups --delete --group %s, so that it no longer counts as behind.",
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
				found = append(found, Finding{
					Severity: Critical,
					Subject:  subject,
					Title:    fmt.Sprintf("%s lost messages of %s to retention", g.Name, r.Topic),
					Explanation: fmt.Sprintf(
						"%s keeps messages for %s, and on %d of its %s it deleted messages %s had not read yet.",
						r.Topic, duration(retention), r.LostPartitions, plural(t.Partitions, "partition"), g.Name),
					Suggestion: fmt.Sprintf(
						"The consumers of %s continue from where auto.offset.reset points them: the oldest message kept, or the newest. Check what the missed messages were for, then keep %s from falling that far behind, or keep the messages of %s longer.",
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

// idleConsumers finds groups with more consumers than partitions to share.
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
		found = append(found, Finding{
			Severity: Info,
			Subject:  Subject{ConsumerGroup: g.Name},
			Title:    fmt.Sprintf("%s has idle consumers", g.Name),
			Explanation: fmt.Sprintf(
				"%s runs %s, but %d of them read nothing: a partition is read by one consumer of a group at a time, and %s %s %s.",
				g.Name, plural(g.Members, "consumer"), g.IdleMembers, list(topics), have(len(topics)), plural(partitions, "partition")),
			Suggestion: fmt.Sprintf(
				"Idle consumers take over when another stops, which may be what you want. To have them share the work, add partitions to %s.",
				list(topics)),
		})
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
			Title:    fmt.Sprintf("%s keeps rebalancing", g.Name),
			Explanation: fmt.Sprintf(
				"The members of %s changed %s in the last hour. Each time, its consumers share the partitions out again, and read less meanwhile.",
				g.Name, plural(g.Rebalances, "time")),
			Suggestion: "Consumers that restart often, or take longer than max.poll.interval.ms between polls, cause this. Give each consumer a group.instance.id, so that a restart keeps its partitions, and keep the work between polls short, for example with smaller batches (max.poll.records).",
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
		if n < 3 {
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
				"One of the %s of %s holds %s of its data on the brokers, where each would hold %s if keys were spread evenly. Messages with the same key always go to the same partition, so a few busy keys load one partition, and the one consumer reading it, more than the rest.",
				plural(n, "partition"), t.Name, percent(largest/total), percent(1/float64(n))),
			Suggestion: "Pick a key with more distinct values, such as an order ID rather than a country, or leave the key out where the order of messages does not matter.",
		})
	}
	return found
}

// producerErrors finds producers whose records fail, or need sending again.
func producerErrors(s Snapshot) []Finding {
	var found []Finding
	for _, p := range s.Producers {
		if p.SendsPerSecond < quietProducer {
			continue
		}
		subject := Subject{ClientID: p.ClientID}
		switch failed, retried := p.ErrorsPerSecond/p.SendsPerSecond, p.RetriesPerSecond/p.SendsPerSecond; {
		case failed >= errorShare:
			found = append(found, Finding{
				Severity: Warning,
				Subject:  subject,
				Title:    fmt.Sprintf("Records from %s fail", p.ClientID),
				Explanation: fmt.Sprintf(
					"%s of the records %s sent in the last %s failed: %s a second of %s.",
					percent(failed), p.ClientID, duration(s.Window.Seconds()), rate(p.ErrorsPerSecond), rate(p.SendsPerSecond)),
				Suggestion: fmt.Sprintf(
					"The log of %s says why. The usual causes are records larger than max.request.size or the topic's max.message.bytes, a topic that does not exist (topics are not created on first use), or %s lacking permission to write to it.",
					p.ClientID, p.Principal),
			})
		case retried >= retryShare:
			found = append(found, Finding{
				Severity: Info,
				Subject:  subject,
				Title:    fmt.Sprintf("%s sends many records twice", p.ClientID),
				Explanation: fmt.Sprintf(
					"%s sent %s of its records again in the last %s. They arrive in the end, but late.",
					p.ClientID, percent(retried), duration(s.Window.Seconds())),
				Suggestion: "Retries follow timeouts and brokers that move partitions, such as during a restart. If they go on, check the network between the producer and the cluster, and request.timeout.ms.",
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
		if t.UnreportedBytesInPerSecond <= unreportedFloor || t.UnreportedBytesInPerSecond <= unreportedShare*t.BytesInPerSecond {
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
