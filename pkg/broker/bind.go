package broker

import (
	osb "github.com/pmorie/go-open-service-broker-client/v2"
	"k8s.io/api/core/v1"
	"k8s.io/api/settings/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"errors"
	"fmt"
	"github.com/golang/glog"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"strings"
	"time"
)

func createBindingParameters(serviceInstance DataSourceInstance,
	bindingParameters map[string]interface{}) map[string]interface{} {

	ds := newDataSource(serviceInstance, bindingParameters)
	credential := make(map[string]interface{})

	credential["database-user"] = ds.Parameters["username"]
	credential["database-password"] = ds.Parameters["password"]
	credential["database-name"] = ds.Parameters["database-name"]
	credential["service-type"] = ds.Parameters["database-type"]
	credential["service-name"] = ds.Parameters["service-name"]
	credential["service-port"] = externalDatabasePort(ds)
	return credential
}

func podPresetAction(bindingAlias string, namespace string, name string, opKey osb.OperationKey,
	fn func(string, string, string) error, after func(osb.OperationKey, error)) {
	// this is hack, really should be driven by some kind of event
	// I have found no such unless we can watch the service bindings
	// come through.
	var err error
	execute := fn(bindingAlias, namespace, name)
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for t := range ticker.C {
			err = execute
			if err == nil {
				t.String()
				err = nil
				ticker.Stop()
				break
			}
		}
	}()
	time.Sleep(30 * time.Second)
	ticker.Stop()
	after(opKey, err)
}

func buildPodPreset(bindAlias string, namespace string, bindName string) error {
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}

	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	// lookthrough all the secrets and find the secret with source name property
	typeMetadata := metav1.TypeMeta{Kind: "Secret", APIVersion: "v1"}
	secretList, err := clientset.CoreV1().Secrets(namespace).List(metav1.ListOptions{TypeMeta: typeMetadata})
	if err != nil {
		fmt.Println("Failed to find the secret with binding-alias field" + i2s(err))
		return err
	}
	presetCreated := false
	for i := range secretList.Items {
		sn, ok := secretList.Items[i].Data["binding-alias"]
		if !ok {
			continue
		}
		srcName := string(sn)
		if srcName == bindAlias {
			properties := secretList.Items[i].Data

			// create PodPreset
			labels := map[string]string{}
			labels["role"] = bindAlias

			pp := v1alpha1.PodPreset{}
			pp.ObjectMeta.Name = bindName
			pp.Spec.Selector = metav1.LabelSelector{MatchLabels: labels}

			envs := []v1.EnvVar{}
			for key := range properties {
				if key == "binding-alias" {
					continue
				}

				ref := v1.LocalObjectReference{Name: secretList.Items[i].ObjectMeta.Name}
				secret := v1.SecretKeySelector{Key: key, LocalObjectReference: ref}
				value := v1.EnvVarSource{SecretKeyRef: &secret}
				key = strings.Replace(key, ".", "_", -1)
				key = strings.Replace(key, "-", "", -1)
				key = strings.ToUpper(key)
				env := v1.EnvVar{Name: key, ValueFrom: &value}
				envs = append(envs, env)
			}
			pp.Spec.Env = envs

			created, err := clientset.SettingsV1alpha1().PodPresets(namespace).Create(&pp)
			fmt.Println("created PodPreset" + i2s(pp))
			if err != nil {
				fmt.Println("Failed to create podpreset after create call " + i2s(err))
				return err
			}
			glog.Infof("created PodPreset" + i2s(created))
			fmt.Println("created PodPreset" + i2s(created))
			presetCreated = true
		}
	}
	if presetCreated {
		return nil
	}
	return errors.New("PodPreset is not created")
}

func removePodPreset(bindAlias string, namespace string, bindName string) error {
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	typeMeta := metav1.TypeMeta{Kind: "PodPreset", APIVersion: "v1"}

	_, err = clientset.SettingsV1alpha1().PodPresets(namespace).Get(bindName, metav1.GetOptions{TypeMeta: typeMeta})
	if err != nil {
		return errors.New("failed to find the PodPreset with name " + bindName + " for removal, with alias " + bindAlias)
	}
	err = clientset.SettingsV1alpha1().PodPresets(namespace).Delete(bindName, &metav1.DeleteOptions{TypeMeta: typeMeta})
	return err
}
