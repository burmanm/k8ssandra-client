package register

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	configapi "github.com/k8ssandra/k8ssandra-operator/apis/config/v1beta1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestRegister(t *testing.T) {
	require := require.New(t)
	client1 := (*multiEnv)[0].GetClientInNamespace("source-namespace")
	client2 := (*multiEnv)[1].GetClientInNamespace("dest-namespace")
	require.NoError(client1.Create((*multiEnv)[0].Context, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "source-namespace"}}))
	require.NoError(client2.Create((*multiEnv)[1].Context, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "dest-namespace"}}))

	testDir, err := os.MkdirTemp("", "k8ssandra-operator-test-****")
	require.NoError(err)
	t.Cleanup(func() {
		require.NoError(os.RemoveAll(testDir))
	})

	kc1, err := (*multiEnv)[0].GetKubeconfig()
	require.NoError(err)
	f1, err := os.Create(testDir + "/kubeconfig1")
	require.NoError(err)
	t.Cleanup(func() {
		require.NoError(f1.Close())
	})
	_, err = f1.Write(kc1)
	require.NoError(err)

	f2, err := os.Create(testDir + "/kubeconfig2")
	require.NoError(err)
	t.Cleanup(func() {
		require.NoError(f2.Close())
	})

	kc2, err := (*multiEnv)[1].GetKubeconfig()
	require.NoError(err)
	_, err = f2.Write(kc2)
	require.NoError(err)

	ex := RegistrationExecutor{
		SourceKubeconfig: testDir + "/kubeconfig1",
		DestKubeconfig:   testDir + "/kubeconfig2",
		SourceContext:    "default-context",
		DestContext:      "default-context",
		SourceNamespace:  "source-namespace",
		DestNamespace:    "dest-namespace",
		ServiceAccount:   "k8ssandra-operator",
		Context:          context.TODO(),
		DestinationName:  "test-destination",
	}
	ctx := context.Background()

	require.Eventually(func() bool {
		if err := ex.RegisterCluster(); err != nil {
			if errors.Is(err, NonRecoverableError{}) {
				require.FailNow(fmt.Sprintf("Registration failed: %s", err.Error()))
			}
			if err.Error() == "no secret found for service account k8ssandra-operator" {
				return true
			}
			return false
		}
		return true
	}, time.Second*5, time.Millisecond*100)

	// This relies on a controller that is not running in the envtest.

	desiredSaSecret := &corev1.Secret{}
	require.NoError(client1.Get(context.Background(), client.ObjectKey{Name: "k8ssandra-operator-secret", Namespace: "source-namespace"}, desiredSaSecret))
	patch := client.MergeFrom(desiredSaSecret.DeepCopy())
	desiredSaSecret.Data = map[string][]byte{
		"token":  []byte("test-token"),
		"ca.crt": []byte("test-ca"),
	}
	require.NoError(client1.Patch(ctx, desiredSaSecret, patch))

	desiredSa := &corev1.ServiceAccount{}
	require.NoError(client1.Get(
		context.Background(),
		client.ObjectKey{Name: "k8ssandra-operator", Namespace: "source-namespace"},
		desiredSa))

	patch = client.MergeFrom(desiredSa.DeepCopy())
	desiredSa.Secrets = []corev1.ObjectReference{
		{
			Name: "k8ssandra-operator-secret",
		},
	}
	require.NoError(client1.Patch(ctx, desiredSa, patch))

	// Continue reconciliation

	require.Eventually(func() bool {
		return ex.RegisterCluster() == nil
	}, time.Second*3, time.Millisecond*100)

	if err := configapi.AddToScheme(client2.Scheme()); err != nil {
		require.NoError(err)
	}
	destSecret := &corev1.Secret{}
	require.Eventually(func() bool {
		err = client2.Get(ctx,
			client.ObjectKey{Name: "test-destination", Namespace: "dest-namespace"}, destSecret)
		if err != nil {
			t.Log("didn't find dest secret")
			return false
		}
		clientConfig := &configapi.ClientConfig{}
		err = client2.Get(ctx,
			client.ObjectKey{Name: "test-destination", Namespace: "dest-namespace"}, clientConfig)
		if err != nil {
			t.Log("didn't find dest client config")
			return false
		}
		return err == nil
	}, time.Second*6, time.Millisecond*100)

	destKubeconfig := ClientConfigFromSecret(destSecret)
	require.Equal(
		desiredSaSecret.Data["ca.crt"],
		destKubeconfig.Clusters["test-destination"].CertificateAuthorityData)

	require.Equal(
		string(desiredSaSecret.Data["token"]),
		destKubeconfig.AuthInfos["test-destination"].Token)
}

func ClientConfigFromSecret(s *corev1.Secret) clientcmdapi.Config {
	out, err := clientcmd.Load(s.Data["kubeconfig"])
	if err != nil {
		panic(err)
	}
	return *out
}
