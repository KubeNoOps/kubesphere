/*
Copyright 2019 The KubeSphere Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package monitoring

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"

	"kubesphere.io/api/iam/v1alpha2"

	ksinformers "kubesphere.io/kubesphere/pkg/client/informers/externalversions"
	"kubesphere.io/kubesphere/pkg/constants"
	"kubesphere.io/kubesphere/pkg/informers"
	"kubesphere.io/kubesphere/pkg/models/observability/monitoring/expressions"
	"kubesphere.io/kubesphere/pkg/simple/client/observability/monitoring"
)

type MonitoringOperator interface {
	GetMetric(expr, namespace string, time time.Time) (monitoring.Metric, error)
	GetMetricOverTime(expr, namespace string, start, end time.Time, step time.Duration) (monitoring.Metric, error)
	GetNamedMetrics(metrics []string, time time.Time, opt monitoring.QueryOption) Metrics
	GetNamedMetricsOverTime(metrics []string, start, end time.Time, step time.Duration, opt monitoring.QueryOption) Metrics
	GetMetadata(namespace string) Metadata
	GetLabelValues(label string, matches []string, start, end time.Time) LabelValues
	GetMetricLabelSet(metric, namespace string, start, end time.Time) MetricLabelSet

	// TODO: expose KubeSphere self metrics in Prometheus format
	GetKubeSphereStats() Metrics
	GetWorkspaceStats(workspace string) Metrics
}

type monitoringOperator struct {
	prometheus monitoring.Interface
	k8s        kubernetes.Interface
	ks         ksinformers.SharedInformerFactory
}

func NewMonitoringOperator(monitoringClient monitoring.Interface, k8s kubernetes.Interface, factory informers.InformerFactory) MonitoringOperator {
	return &monitoringOperator{
		prometheus: monitoringClient,
		k8s:        k8s,
		ks:         factory.KubeSphereSharedInformerFactory(),
	}
}

func (mo monitoringOperator) GetMetric(expr, namespace string, time time.Time) (monitoring.Metric, error) {
	return mo.prometheus.GetMetric(expr, time), nil
}

func (mo monitoringOperator) GetMetricOverTime(expr, namespace string, start, end time.Time, step time.Duration) (monitoring.Metric, error) {

	return mo.prometheus.GetMetricOverTime(expr, start, end, step), nil
}

func (mo monitoringOperator) GetNamedMetrics(metrics []string, time time.Time, opt monitoring.QueryOption) Metrics {
	ress := mo.prometheus.GetNamedMetrics(metrics, time, opt)

	return Metrics{Results: ress}
}

func (mo monitoringOperator) GetNamedMetricsOverTime(metrics []string, start, end time.Time, step time.Duration, opt monitoring.QueryOption) Metrics {
	ress := mo.prometheus.GetNamedMetricsOverTime(metrics, start, end, step, opt)

	return Metrics{Results: ress}
}

func (mo monitoringOperator) GetMetadata(namespace string) Metadata {
	data := mo.prometheus.GetMetadata(namespace)
	return Metadata{Data: data}
}

func (mo monitoringOperator) GetLabelValues(label string, matches []string, start, end time.Time) LabelValues {
	data := mo.prometheus.GetLabelValues(label, matches, start, end)
	return LabelValues{Data: data}
}

func (mo monitoringOperator) GetMetricLabelSet(metric, namespace string, start, end time.Time) MetricLabelSet {
	var expr = metric
	var err error
	if namespace != "" {
		// Different monitoring backend implementations have different ways to enforce namespace isolation.
		// Each implementation should register itself to `ReplaceNamespaceFns` during init().
		// We hard code "prometheus" here because we only support this datasource so far.
		// In the future, maybe the value should be returned from a method like `mo.c.GetMonitoringServiceName()`.
		expr, err = expressions.ReplaceNamespaceFns["prometheus"](metric, namespace)
		if err != nil {
			klog.Error(err)
			return MetricLabelSet{}
		}
	}
	data := mo.prometheus.GetMetricLabelSet(expr, start, end)
	return MetricLabelSet{Data: data}
}

func (mo monitoringOperator) GetKubeSphereStats() Metrics {
	var res Metrics
	now := float64(time.Now().Unix())

	clusterList, err := mo.ks.Cluster().V1alpha1().Clusters().Lister().List(labels.Everything())
	clusterTotal := len(clusterList)
	if clusterTotal == 0 {
		clusterTotal = 1
	}
	if err != nil {
		res.Results = append(res.Results, monitoring.Metric{
			MetricName: KubeSphereClusterCount,
			Error:      err.Error(),
		})
	} else {
		res.Results = append(res.Results, monitoring.Metric{
			MetricName: KubeSphereClusterCount,
			MetricData: monitoring.MetricData{
				MetricType: monitoring.MetricTypeVector,
				MetricValues: []monitoring.MetricValue{
					{
						Sample: &monitoring.Point{now, float64(clusterTotal)},
					},
				},
			},
		})
	}

	wkList, err := mo.ks.Tenant().V1alpha2().WorkspaceTemplates().Lister().List(labels.Everything())
	if err != nil {
		res.Results = append(res.Results, monitoring.Metric{
			MetricName: KubeSphereWorkspaceCount,
			Error:      err.Error(),
		})
	} else {
		res.Results = append(res.Results, monitoring.Metric{
			MetricName: KubeSphereWorkspaceCount,
			MetricData: monitoring.MetricData{
				MetricType: monitoring.MetricTypeVector,
				MetricValues: []monitoring.MetricValue{
					{
						Sample: &monitoring.Point{now, float64(len(wkList))},
					},
				},
			},
		})
	}

	usrList, err := mo.ks.Iam().V1alpha2().Users().Lister().List(labels.Everything())
	if err != nil {
		res.Results = append(res.Results, monitoring.Metric{
			MetricName: KubeSphereUserCount,
			Error:      err.Error(),
		})
	} else {
		res.Results = append(res.Results, monitoring.Metric{
			MetricName: KubeSphereUserCount,
			MetricData: monitoring.MetricData{
				MetricType: monitoring.MetricTypeVector,
				MetricValues: []monitoring.MetricValue{
					{
						Sample: &monitoring.Point{now, float64(len(usrList))},
					},
				},
			},
		})
	}

	return res
}

func (mo monitoringOperator) GetWorkspaceStats(workspace string) Metrics {
	var res Metrics
	now := float64(time.Now().Unix())

	selector := labels.SelectorFromSet(labels.Set{constants.WorkspaceLabelKey: workspace})
	opt := metav1.ListOptions{LabelSelector: selector.String()}

	nsList, err := mo.k8s.CoreV1().Namespaces().List(context.Background(), opt)
	if err != nil {
		res.Results = append(res.Results, monitoring.Metric{
			MetricName: WorkspaceNamespaceCount,
			Error:      err.Error(),
		})
	} else {
		res.Results = append(res.Results, monitoring.Metric{
			MetricName: WorkspaceNamespaceCount,
			MetricData: monitoring.MetricData{
				MetricType: monitoring.MetricTypeVector,
				MetricValues: []monitoring.MetricValue{
					{
						Sample: &monitoring.Point{now, float64(len(nsList.Items))},
					},
				},
			},
		})
	}

	devopsList, err := mo.ks.Devops().V1alpha3().DevOpsProjects().Lister().List(selector)
	if err != nil {
		res.Results = append(res.Results, monitoring.Metric{
			MetricName: WorkspaceDevopsCount,
			Error:      err.Error(),
		})
	} else {
		res.Results = append(res.Results, monitoring.Metric{
			MetricName: WorkspaceDevopsCount,
			MetricData: monitoring.MetricData{
				MetricType: monitoring.MetricTypeVector,
				MetricValues: []monitoring.MetricValue{
					{
						Sample: &monitoring.Point{now, float64(len(devopsList))},
					},
				},
			},
		})
	}

	r, _ := labels.NewRequirement(v1alpha2.UserReferenceLabel, selection.Exists, nil)
	memberSelector := selector.DeepCopySelector().Add(*r)
	memberList, err := mo.ks.Iam().V1alpha2().WorkspaceRoleBindings().Lister().List(memberSelector)
	if err != nil {
		res.Results = append(res.Results, monitoring.Metric{
			MetricName: WorkspaceMemberCount,
			Error:      err.Error(),
		})
	} else {
		res.Results = append(res.Results, monitoring.Metric{
			MetricName: WorkspaceMemberCount,
			MetricData: monitoring.MetricData{
				MetricType: monitoring.MetricTypeVector,
				MetricValues: []monitoring.MetricValue{
					{
						Sample: &monitoring.Point{now, float64(len(memberList))},
					},
				},
			},
		})
	}

	roleList, err := mo.ks.Iam().V1alpha2().WorkspaceRoles().Lister().List(selector)
	if err != nil {
		res.Results = append(res.Results, monitoring.Metric{
			MetricName: WorkspaceRoleCount,
			Error:      err.Error(),
		})
	} else {
		res.Results = append(res.Results, monitoring.Metric{
			MetricName: WorkspaceRoleCount,
			MetricData: monitoring.MetricData{
				MetricType: monitoring.MetricTypeVector,
				MetricValues: []monitoring.MetricValue{
					{
						Sample: &monitoring.Point{now, float64(len(roleList))},
					},
				},
			},
		})
	}

	return res
}