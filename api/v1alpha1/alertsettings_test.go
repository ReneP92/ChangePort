package v1alpha1_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"

	"github.com/ReneP92/changeport/api/v1alpha1"
)

// The CRD fills in what DefaultAlertSettings says a customer without
// AlertSettings gets, so that the console and the rules agree.
func TestAlertSettingsDefaultsAreTheCRDs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "deploy", "base", "provisioner", "changeport.io_alertsettings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatal(err)
	}
	spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
	// The defaults of each alert's fields, as the API server applies them to
	// an empty spec.
	defaulted := map[string]map[string]any{}
	for alert, schema := range spec.Properties {
		if alert == "receivers" {
			continue
		}
		defaulted[alert] = map[string]any{}
		for field, property := range schema.Properties {
			if property.Default == nil {
				t.Errorf("%s.%s has no default", alert, field)
				continue
			}
			var value any
			if err := json.Unmarshal(property.Default.Raw, &value); err != nil {
				t.Fatal(err)
			}
			defaulted[alert][field] = value
		}
	}
	data, err = json.Marshal(defaulted)
	if err != nil {
		t.Fatal(err)
	}
	var got v1alpha1.AlertSettingsSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != v1alpha1.DefaultAlertSettings {
		t.Errorf("the CRD's defaults are %+v, want DefaultAlertSettings %+v", got, v1alpha1.DefaultAlertSettings)
	}
}
