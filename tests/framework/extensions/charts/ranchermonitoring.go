package charts

import (
	"context"
	"fmt"

	"github.com/rancher/rancher/pkg/api/steve/catalog/types"
	catalogv1 "github.com/rancher/rancher/pkg/apis/catalog.cattle.io/v1"
	"github.com/rancher/rancher/tests/framework/clients/rancher"
	"github.com/rancher/rancher/tests/framework/extensions/namespaces"
	"github.com/rancher/rancher/tests/framework/pkg/wait"
	"github.com/rancher/rancher/tests/integration/pkg/defaults"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

const (
	// Namespace that rancher monitoring chart is installed in
	RancherMonitoringNamespace = "cattle-monitoring-system"
	// Name of the rancher monitoring chart
	RancherMonitoringName = "rancher-monitoring"
)

// InstallRancherMonitoringChart is a helper function that installs the rancher-monitoring chart.
func InstallRancherMonitoringChart(client *rancher.Client, installOptions *InstallOptions, rancherMonitoringOpts *RancherMonitoringOpts) error {
	monitoringChartInstallActionPayload := &payloadOpts{
		InstallOptions: *installOptions,
		Name:           RancherMonitoringName,
		Host:           client.RancherConfig.Host,
		Namespace:      RancherMonitoringNamespace,
	}

	chartInstallAction := newMonitoringChartInstallAction(monitoringChartInstallActionPayload, rancherMonitoringOpts)

	catalogClient, err := client.GetClusterCatalogClient(installOptions.ClusterID)
	if err != nil {
		return err
	}

	// Cleanup registration
	client.Session.RegisterCleanupFunc(func() error {
		// UninstallAction for when uninstalling the rancher-monitoring chart
		defaultChartUninstallAction := newChartUninstallAction()

		err = catalogClient.UninstallChart(RancherMonitoringName, RancherMonitoringNamespace, defaultChartUninstallAction)
		if err != nil {
			return err
		}

		watchAppInterface, err := catalogClient.Apps(RancherMonitoringNamespace).Watch(context.TODO(), metav1.ListOptions{
			FieldSelector:  "metadata.name=" + RancherMonitoringName,
			TimeoutSeconds: &defaults.WatchTimeoutSeconds,
		})
		if err != nil {
			return err
		}

		err = wait.WatchWait(watchAppInterface, func(event watch.Event) (ready bool, err error) {
			if event.Type == watch.Error {
				return false, fmt.Errorf("there was an error uninstalling rancher monitoring chart")
			} else if event.Type == watch.Deleted {
				return true, nil
			}
			return false, nil
		})
		if err != nil {
			return err
		}

		dynamicClient, err := client.GetDownStreamClusterClient(installOptions.ClusterID)
		if err != nil {
			return err
		}
		namespaceResource := dynamicClient.Resource(namespaces.NamespaceGroupVersionResource).Namespace("")

		err = namespaceResource.Delete(context.TODO(), RancherMonitoringNamespace, metav1.DeleteOptions{})
		if errors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}

		adminClient, err := rancher.NewClient(client.RancherConfig.AdminToken, client.Session)
		if err != nil {
			return err
		}
		adminDynamicClient, err := adminClient.GetDownStreamClusterClient(installOptions.ClusterID)
		if err != nil {
			return err
		}
		adminNamespaceResource := adminDynamicClient.Resource(namespaces.NamespaceGroupVersionResource).Namespace("")

		watchNamespaceInterface, err := adminNamespaceResource.Watch(context.TODO(), metav1.ListOptions{
			FieldSelector:  "metadata.name=" + RancherMonitoringNamespace,
			TimeoutSeconds: &defaults.WatchTimeoutSeconds,
		})

		if err != nil {
			return err
		}

		return wait.WatchWait(watchNamespaceInterface, func(event watch.Event) (ready bool, err error) {
			if event.Type == watch.Deleted {
				return true, nil
			}
			return false, nil
		})
	})

	err = catalogClient.InstallChart(chartInstallAction)
	if err != nil {
		return err
	}

	// wait for chart to be full deployed
	watchAppInterface, err := catalogClient.Apps(RancherMonitoringNamespace).Watch(context.TODO(), metav1.ListOptions{
		FieldSelector:  "metadata.name=" + RancherMonitoringName,
		TimeoutSeconds: &defaults.WatchTimeoutSeconds,
	})
	if err != nil {
		return err
	}

	err = wait.WatchWait(watchAppInterface, func(event watch.Event) (ready bool, err error) {
		app := event.Object.(*catalogv1.App)

		state := app.Status.Summary.State
		if state == string(catalogv1.StatusDeployed) {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	return nil
}

// newMonitoringChartInstallAction is a private helper function that returns chart install action with monitoring and payload options.
func newMonitoringChartInstallAction(p *payloadOpts, rancherMonitoringOpts *RancherMonitoringOpts) *types.ChartInstallAction {
	monitoringValues := map[string]interface{}{
		"ingressNgnix": map[string]interface{}{
			"enabled": rancherMonitoringOpts.IngressNginx,
		},
		"prometheus": map[string]interface{}{
			"prometheusSpec": map[string]interface{}{
				"evaluationInterval": "1m",
				"retentionSize":      "50GiB",
				"scrapeInterval":     "1m",
			},
		},
		"rkeControllerManager": map[string]interface{}{
			"enabled": rancherMonitoringOpts.RKEControllerManager,
		},
		"rkeEtcd": map[string]interface{}{
			"enabled": rancherMonitoringOpts.RKEEtcd,
		},
		"rkeProxy": map[string]interface{}{
			"enabled": rancherMonitoringOpts.RKEProxy,
		},
		"rkeScheduler": map[string]interface{}{
			"enabled": rancherMonitoringOpts.RKEScheduler,
		},
	}

	chartInstall := newChartInstall(p.Name, p.InstallOptions.Version, p.InstallOptions.ClusterID, p.InstallOptions.ClusterName, p.Host, monitoringValues)
	chartInstallCRD := newChartInstall(p.Name+"-crd", p.Version, p.InstallOptions.ClusterID, p.InstallOptions.ClusterName, p.Host, nil)
	chartInstalls := []types.ChartInstall{*chartInstallCRD, *chartInstall}

	chartInstallAction := newChartInstallAction(p.Namespace, p.ProjectID, chartInstalls)

	return chartInstallAction
}
