// Package diagnosis explains the common Kafka problems of a customer's
// cluster in plain language (docs/diagnosis.md): each rule looks at a
// snapshot of the cluster, its metrics over the last minutes and what Kafka
// says of its topics and consumer groups, and returns what it finds wrong,
// with the numbers that show it and what to do about it.
package diagnosis

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// Snapshot is what the rules look at.
type Snapshot struct {
	// Window is how far back its rates and changes look, so that a
	// moment's spike or dip is no diagnosis.
	Window    time.Duration
	Topics    []Topic
	Groups    []Group
	Producers []Producer
	// Transactions are those open now.
	Transactions []Transaction
}

// Transaction is a transaction open on the cluster.
type Transaction struct {
	// ID is its transactional ID.
	ID string
	// State is its coordinator's: Ongoing, PrepareCommit or PrepareAbort.
	State string
	// Open is how long it has been open, and Timeout how long it may be
	// before its coordinator aborts it.
	Open, Timeout time.Duration
	Partitions    []TransactionPartition
}

// TransactionPartition is a partition a transaction wrote to, with the
// messages read_committed consumers cannot read yet: those after its last
// stable offset.
type TransactionPartition struct {
	Topic     string
	Partition int32
	HeldBack  int64
}

// HangingTransaction is a transaction open on a partition that its
// coordinator no longer has open there, as kafka-transactions.sh
// find-hanging tells it: Kafka never aborts it by itself.
type HangingTransaction struct {
	Partition  int32
	ProducerID int64
	// TransactionalID names its producer, when Kafka still knows it.
	TransactionalID string
	// From is the offset of its first message, and Open how long it has at
	// least been open from there.
	From int64
	Open time.Duration
}

// Topic is one of the customer's topics.
type Topic struct {
	Name       string
	Partitions int
	// RetentionMs is how long the topic keeps messages, or nil for ever.
	RetentionMs *int64
	// Compacted topics keep the latest message per key however old.
	Compacted bool
	// Tiered topics keep only their last day on the brokers' disks, so
	// PartitionBytes shows how the last day's messages spread.
	Tiered bool
	// PartitionsAdded is whether the topic got partitions in the last day,
	// which start empty.
	PartitionsAdded bool
	// MaxMessageBytes is the largest message the topic accepts
	// (max.message.bytes), or 0 when unknown.
	MaxMessageBytes int64
	// BytesRejected is what the topic refused as larger than that over the
	// window.
	BytesRejected float64
	// IdempotentProducers are the producers Kafka keeps state for that
	// wrote to the topic since the console's last look: idempotent or
	// transactional ones, whose sequence moved on. Kafka keeps none for
	// other producers. Nil when not known.
	IdempotentProducers *int
	// Hanging are the topic's hanging transactions.
	Hanging []HangingTransaction
	// Source is whether a Postgres source writes the topic: Changeport's
	// own Connect worker, which sends no telemetry.
	Source bool
	// MessagesInPerSecond and BytesInPerSecond are what the topic received.
	MessagesInPerSecond float64
	BytesInPerSecond    float64
	// UnreportedBytesInPerSecond is what reached the topic from producers
	// that send no telemetry (KIP-714).
	UnreportedBytesInPerSecond float64
	// PartitionBytes is the size of each partition on the brokers' disks.
	PartitionBytes []float64
}

// Group is a consumer group.
type Group struct {
	Name string
	// State is Kafka's: Empty, Stable, PreparingRebalance and so on.
	State   string
	Members int
	// IdleMembers are members assigned no partition at all.
	IdleMembers int
	// Rebalances is how often it shared its partitions out again in the
	// last hour, as members joined or left while it consumed.
	Rebalances int
	// Moved is how an admin last moved the group in the console, or created
	// it to replay another's topics, in the last hour; nil if none did.
	Moved *Move
	Reads []Read
}

// Move is an admin moving a consumer group in the console, so that it reads
// messages again or skips them, or creating one to replay another's topics.
type Move struct {
	// By is who, and Ago how long ago.
	By  string
	Ago time.Duration
	// Replay is whether it was created to replay another group's topics.
	Replay bool
}

// words says who moved the group and when, as in "Ada moved billing in the
// console 4 minutes ago".
func (m Move) words(group string) string {
	by := cmp.Or(m.By, "an admin")
	if m.Replay {
		return fmt.Sprintf("%s created %s in the console %s ago to replay messages", by, group, behind(m.Ago.Seconds()))
	}
	return fmt.Sprintf("%s moved %s in the console %s ago", by, group, behind(m.Ago.Seconds()))
}

// Read is a consumer group reading a topic.
type Read struct {
	Topic string
	// LagSeconds is the age of the oldest message the group has not read;
	// capped means it is older than the agent looks back (23 hours).
	LagSeconds  float64
	LagMessages int64
	Capped      bool
	// LagSecondsEarlier was the lag a window ago, when known.
	LagSecondsEarlier *float64
	// ConsumedPerSecond is the messages a second the group consumed of the
	// topic over the window, from its committed offsets.
	ConsumedPerSecond float64
	// AssignedMembers are the group's members reading the topic.
	AssignedMembers int
	// LostPartitions are partitions where retention deleted messages the
	// group never read.
	LostPartitions int
	// OffsetsReset is whether the group's offsets went back in the window,
	// as when it replays a topic: its pace then reads as nothing.
	OffsetsReset bool
}

// Producer is a client writing to the cluster, as it reports itself.
type Producer struct {
	Principal string
	ClientID  string
	// The records a second it sent, failed to send and sent again, over
	// the window.
	SendsPerSecond   float64
	ErrorsPerSecond  float64
	RetriesPerSecond float64
	// TopicErrorsPerSecond are the records a second that failed on their
	// way to a topic or at it, by topic: refused by Kafka, or expired. The
	// rest of its errors are records the producer refused itself, before
	// sending them.
	TopicErrorsPerSecond map[string]float64
	// AbortedTransactions is whether it spent time aborting transactions
	// over the window. Kafka counts aborts nowhere, so not how many.
	AbortedTransactions bool
}

// Severity is how much a finding matters.
type Severity string

const (
	// Info is worth knowing, and may be fine.
	Info Severity = "info"
	// Warning needs attention before it hurts.
	Warning Severity = "warning"
	// Critical hurts now: data is lost or nothing is consumed.
	Critical Severity = "critical"
)

func (s Severity) rank() int {
	return slices.Index([]Severity{Info, Warning, Critical}, s)
}

// Subject is what a finding is about.
type Subject struct {
	// Topic, ConsumerGroup and ClientID name it; a group reading a topic
	// has both, and a client its Kafka user. A transaction holding back a
	// topic has the topic and its transactional ID.
	Topic           string
	ConsumerGroup   string
	ClientID        string
	Principal       string
	TransactionalID string
}

// Finding is one problem a rule found.
type Finding struct {
	// ID identifies the finding while it holds, so that the console can
	// tell it is the same one: the rule and its subject.
	ID       string
	Rule     string
	Severity Severity
	Subject  Subject
	// Title says what is wrong in a line; Explanation shows it with the
	// numbers; Suggestion says what to do.
	Title       string
	Explanation string
	Suggestion  string
	// Docs is the rule's page in the customer docs, relative to their
	// address: site/content/docs/troubleshooting/<rule>.md, rendered.
	Docs string
}

// Rule finds one kind of problem.
type Rule struct {
	Name string
	Find func(Snapshot) []Finding
}

// Rules are every rule, in the order docs/diagnosis.md describes them.
var Rules = []Rule{
	{"consumer-too-slow", consumerTooSlow},
	{"nobody-consuming", nobodyConsuming},
	{"behind-retention", behindRetention},
	{"idle-consumers", idleConsumers},
	{"frequent-rebalances", frequentRebalances},
	{"hot-partition", hotPartition},
	{"partitions-added", partitionsAdded},
	{"oversized-messages", oversizedMessages},
	{"producer-errors", producerErrors},
	{"producers-without-telemetry", producersWithoutTelemetry},
	{"hanging-transaction", hangingTransaction},
	{"open-transaction", openTransaction},
	{"transactions-aborted", transactionsAborted},
	{"producers-not-idempotent", producersNotIdempotent},
}

// Diagnose runs every rule over the snapshot and returns what they found,
// the most severe first.
func Diagnose(s Snapshot) []Finding {
	findings := []Finding{}
	for _, rule := range Rules {
		for _, f := range rule.Find(s) {
			f.Rule = rule.Name
			f.Docs = "troubleshooting/" + rule.Name + ".html"
			f.ID = rule.Name + ":" + strings.Join(slices.DeleteFunc(
				[]string{f.Subject.ConsumerGroup, f.Subject.Topic, f.Subject.TransactionalID, f.Subject.Principal, f.Subject.ClientID},
				func(s string) bool { return s == "" }), "/")
			findings = append(findings, f)
		}
	}
	slices.SortStableFunc(findings, func(a, b Finding) int {
		return cmp.Or(cmp.Compare(b.Severity.rank(), a.Severity.rank()), cmp.Compare(a.ID, b.ID))
	})
	return findings
}

// topic returns the snapshot's topic of that name.
func (s Snapshot) topic(name string) (Topic, bool) {
	i := slices.IndexFunc(s.Topics, func(t Topic) bool { return t.Name == name })
	if i < 0 {
		return Topic{}, false
	}
	return s.Topics[i], true
}

// growing is whether the lag grew over the window, by more than the agent's
// measurements wander. Lag older than the agent looks back reads the same
// each time, so it counts as growing while messages wait.
func (r Read) growing() bool {
	if r.Capped {
		return r.LagMessages > 0
	}
	return r.LagSecondsEarlier != nil && r.LagSeconds > *r.LagSecondsEarlier+30
}

// plural is "1 partition" or "3 partitions".
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// duration says how long, as the console does (web/src/lib/format.ts): 45
// seconds, 3 minutes, 1 hour 20 minutes, 12 hours, 3 days.
func duration(seconds float64) string {
	switch {
	case seconds < 60:
		return plural(int(seconds), "second")
	case seconds < 3600:
		return plural(int(seconds/60), "minute")
	case seconds < 48*3600:
		hours, minutes := int(seconds/3600), int(math.Mod(seconds, 3600)/60)
		if hours >= 10 || minutes == 0 {
			return plural(hours, "hour")
		}
		return plural(hours, "hour") + " " + plural(minutes, "minute")
	default:
		return plural(int(seconds/86400), "day")
	}
}

// rate says how many messages a second, to two significant digits, with
// thousands set apart: 1,250.
func rate(perSecond float64) string {
	switch {
	case perSecond >= 10:
		digits := fmt.Sprintf("%.0f", perSecond)
		for i := len(digits) - 3; i > 0; i -= 3 {
			digits = digits[:i] + "," + digits[i:]
		}
		return digits
	case perSecond >= 1:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", perSecond), ".0")
	default:
		return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", perSecond), "0"), ".")
	}
}

// upperFirst starts a sentence with a capital.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// behind says how far behind, with less than a minute as such.
func behind(seconds float64) string {
	if seconds < 60 {
		return "less than a minute"
	}
	return duration(seconds)
}

// bytes says how many bytes, in powers of 1,000, as the console does.
func bytes(n float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	unit := 0
	for n >= 1000 && unit < len(units)-1 {
		n /= 1000
		unit++
	}
	if unit == 0 || n >= 100 {
		return fmt.Sprintf("%.0f %s", n, units[unit])
	}
	return fmt.Sprintf("%.1f %s", n, units[unit])
}

// percent says a share as a whole percentage, or "less than 1%".
func percent(share float64) string {
	if share < 0.01 {
		return "less than 1%"
	}
	return fmt.Sprintf("%.0f%%", share*100)
}

// Level is how healthy a topic or consumer group is, from its findings.
type Level int

const (
	// Healthy has no findings but notes, which may be fine.
	Healthy Level = iota
	// NeedsAttention has warnings.
	NeedsAttention
	// AtRisk has something critical.
	AtRisk
)

// Health is a topic's or a group's level, with the IDs of the findings
// that set it, so that a level always comes with its reasons.
type Health struct {
	Level    Level
	Findings []string
}

// Levels gives every topic and consumer group a finding is about its level
// (docs/diagnosis.md). A finding about a group reading a topic is the
// group's: the group falls behind, not the topic. Notes leave a subject
// healthy. Subjects without findings are healthy, and left out.
func Levels(findings []Finding) (topics, groups map[string]Health) {
	topics, groups = map[string]Health{}, map[string]Health{}
	for _, f := range findings {
		level := Healthy
		switch f.Severity {
		case Warning:
			level = NeedsAttention
		case Critical:
			level = AtRisk
		}
		if level == Healthy {
			continue
		}
		into, name := topics, f.Subject.Topic
		if f.Subject.ConsumerGroup != "" {
			into, name = groups, f.Subject.ConsumerGroup
		}
		if name == "" {
			continue
		}
		h := into[name]
		h.Level = max(h.Level, level)
		h.Findings = append(h.Findings, f.ID)
		into[name] = h
	}
	return topics, groups
}
