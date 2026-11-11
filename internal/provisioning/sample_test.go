package provisioning

import (
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestRenderSampleAppOnlyWhenRunning(t *testing.T) {
	if sample := splitParts(renderSpec(t, acme))[PartSample]; len(sample) != 0 {
		t.Errorf("a customer without the sample app renders %d of its objects", len(sample))
	}
	// Without a published Kafka image, the platform has no sample app.
	eks := Spec{CustomerID: "acme", Profile: ProductionProfile, Platform: EKSProdPlatform, Sample: true}
	eks.Platform.KafkaImage = ""
	if sample := splitParts(renderSpec(t, eks))[PartSample]; len(sample) != 0 {
		t.Errorf("a platform without a Kafka image renders %d sample app objects", len(sample))
	}

	running := acme
	running.Sample = true
	var kinds []string
	for _, obj := range splitParts(renderSpec(t, running))[PartSample] {
		kinds = append(kinds, obj.GetKind()+" "+obj.GetName())
		if !slices.ContainsFunc(prunedKinds, func(kind schema.GroupVersionKind) bool { return kind.Kind == obj.GetKind() }) {
			t.Errorf("%s %s is not of a kind the provisioner prunes, so stopping the sample would leave it", obj.GetKind(), obj.GetName())
		}
	}
	want := []string{"KafkaUser " + SampleUser, "KafkaTopic " + SampleTopic, "Deployment " + SampleApp}
	if !slices.Equal(kinds, want) {
		t.Errorf("the sample app renders %v, want %v", kinds, want)
	}
}

// The sample app's user may only write and read its own topic, in its own
// consumer groups: never the customer's topics.
func TestRenderSampleUserOnlyTouchesItsTopic(t *testing.T) {
	running := acme
	running.Sample = true
	var user *unstructured.Unstructured
	for _, obj := range renderSpec(t, running) {
		if obj.GetKind() == "KafkaUser" && obj.GetName() == SampleUser {
			user = obj
		}
	}
	if user == nil {
		t.Fatal("no sample user")
	}
	if got, _, _ := unstructured.NestedString(user.Object, "spec", "authentication", "type"); got != "scram-sha-512" {
		t.Errorf("the sample user authenticates with %q, want scram-sha-512 like the customer's own clients", got)
	}
	acls, _, err := unstructured.NestedSlice(user.Object, "spec", "authorization", "acls")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range acls {
		acl, _ := a.(map[string]any)
		kind, _, _ := unstructured.NestedString(acl, "resource", "type")
		name, _, _ := unstructured.NestedString(acl, "resource", "name")
		pattern, _, _ := unstructured.NestedString(acl, "resource", "patternType")
		operations, _, _ := unstructured.NestedStringSlice(acl, "operations")
		switch {
		case kind == "topic" && name == SampleTopic && pattern == "literal" && slices.Equal(operations, []string{"Describe", "Read", "Write"}):
		case kind == "group" && name == SampleGroupPrefix && pattern == "prefix" && slices.Equal(operations, []string{"Read"}):
		case kind == "cluster" && slices.Equal(operations, []string{"IdempotentWrite"}):
		default:
			t.Errorf("the sample user has ACL %s %q (%s) %v beyond its own topic and groups", kind, name, pattern, operations)
		}
	}
}

// Every cluster has the internal listener with passwords that the sample
// app connects to, so that starting it never restarts the brokers.
func TestRenderScramListenerOnEveryProfile(t *testing.T) {
	for _, profile := range sortedValues(Profiles) {
		spec := Spec{CustomerID: "acme", Profile: profile, Platform: EKSProdPlatform}
		for _, obj := range renderSpec(t, spec) {
			if obj.GetKind() != "Kafka" {
				continue
			}
			listeners, _, err := unstructured.NestedSlice(obj.Object, "spec", "kafka", "listeners")
			if err != nil {
				t.Fatal(err)
			}
			found := slices.ContainsFunc(listeners, func(l any) bool {
				listener, _ := l.(map[string]any)
				// The decoder reads numbers as JSON does.
				port, _, _ := unstructured.NestedFloat64(listener, "port")
				kind, _, _ := unstructured.NestedString(listener, "type")
				authentication, _, _ := unstructured.NestedString(listener, "authentication", "type")
				return port == ScramPort && kind == "internal" && authentication == "scram-sha-512"
			})
			if !found {
				t.Errorf("profile %s has no internal SCRAM listener on port %d", profile.Name, ScramPort)
			}
		}
	}
}
