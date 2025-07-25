package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/argoproj/notifications-engine/pkg/api"
	"github.com/argoproj/notifications-engine/pkg/services"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"
)

func newTestResource(name string) *unstructured.Unstructured {
	res := unstructured.Unstructured{}
	res.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Kind: "application", Version: "v1alpha1"})
	res.SetName(name)
	res.SetNamespace("default")
	return &res
}

func newTestContext(stdout io.Writer, stderr io.Writer, data map[string]string, resources ...runtime.Object) (*commandContext, func(), error) {
	cm := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-config-map",
		},
		Data: data,
	}
	cmData, err := yaml.Marshal(cm)
	if err != nil {
		return nil, nil, err
	}
	tmpFile, err := os.CreateTemp("", "*-cm.yaml")
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = tmpFile.Close()
	}()
	_, err = tmpFile.Write(cmData)
	if err != nil {
		return nil, nil, err
	}

	ctx := &commandContext{
		stdout:        stdout,
		stderr:        stderr,
		stdin:         strings.NewReader(""),
		secretPath:    ":empty",
		configMapPath: tmpFile.Name(),
		resource:      schema.GroupVersionResource{Group: "argoproj.io", Resource: "applications", Version: "v1alpha1"},
		dynamicClient: dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
			schema.GroupVersionResource{Group: "argoproj.io", Resource: "applications", Version: "v1alpha1"}: "List",
			schema.GroupVersionResource{Group: "argoproj.io", Resource: "appprojects", Version: "v1alpha1"}:  "List",
		}, resources...),
		k8sClient: fake.NewSimpleClientset(),
		namespace: "default",
		cliName:   "argocd-notifications",
		Settings: api.Settings{
			ConfigMapName: "my-config-map",
			SecretName:    "my-secret",
			InitGetVars: func(cfg *api.Config, configMap *corev1.ConfigMap, secret *corev1.Secret) (api.GetVars, error) {
				return func(obj map[string]interface{}, _ services.Destination) map[string]interface{} {
					return map[string]interface{}{"app": obj}
				}, nil
			},
		},
	}
	return ctx, func() {
		_ = os.RemoveAll(tmpFile.Name())
	}, nil
}

func TestTriggerRun(t *testing.T) {
	cmData := map[string]string{
		"trigger.my-trigger": `
- when: app.metadata.name == 'guestbook'
  send: [my-template]`,
		"template.my-template": `
message: hello {{.app.metadata.name}}`,
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	ctx, closer, err := newTestContext(&stdout, &stderr, cmData, newTestResource("guestbook"))
	if !assert.NoError(t, err) {
		return
	}
	defer closer()

	command := newTriggerRunCommand(ctx)
	err = command.RunE(command, []string{"my-trigger", "guestbook"})
	assert.NoError(t, err)
	assert.Empty(t, stderr.String())
	assert.Contains(t, stdout.String(), "true")
}

func TestTriggerGet(t *testing.T) {
	cmData := map[string]string{
		"trigger.my-trigger1": `
- when: 'true'
  send: [my-template]`,
		"trigger.my-trigger2": `
- when: 'false'
  send: [my-template]`,
		"template.my-template": `
message: hello {{.app.metadata.name}}`,
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	ctx, closer, err := newTestContext(&stdout, &stderr, cmData)
	if !assert.NoError(t, err) {
		return
	}
	defer closer()

	command := newTriggerGetCommand(ctx)
	err = command.RunE(command, nil)
	assert.NoError(t, err)
	assert.Empty(t, stderr.String())
	assert.Contains(t, stdout.String(), "my-trigger1")
	assert.Contains(t, stdout.String(), "my-trigger2")
}
