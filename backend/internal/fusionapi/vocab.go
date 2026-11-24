package fusionapi

import "strings"

// Label is one resource attribute that travels from a sender to a query, and what each store makes of it. This file is
// the one place the vocabulary is spelled: the Go names below are derived from it, and the doctor (internal/server/
// doctor_vocab_test.go) holds the charts and the dashboards to it. To add a label, add a line here and let the doctor
// say which chart value to change.
type Label struct {
	Attr string // the OTLP resource attribute
	// Promote: Prometheus turns it into a label (prometheus.promoteResourceAttributes in the FUSION chart).
	Promote bool
	// Index: Loki keeps it as a stream label, so a selector may name it; otherwise it is structured metadata, which only a
	// filter after the pipe can read.
	Index bool
	// By is who stamps it as provenance: "agent" (resource/ikhnos), "extractor" (the agent's
	// transform/ikhnos_source_* processors, one per pipeline), "operator" (resource/operator) or "gateway"
	// (transform/category). Empty: the sender or the collectors' own enrichment, nothing of ours.
	By string
}

// Name is the label both stores give the attribute: OTLP's own translation turns the dots into underscores.
func (l Label) Name() string { return strings.ReplaceAll(l.Attr, ".", "_") }

var (
	vocService     = Label{Attr: "service.name", Promote: true, Index: true}
	vocInstance    = Label{Attr: "service.instance.id", Index: true}
	vocNamespace   = Label{Attr: "k8s.namespace.name", Promote: true, Index: true}
	vocPod         = Label{Attr: "k8s.pod.name", Promote: true, Index: true}
	vocNode        = Label{Attr: "k8s.node.name", Promote: true}
	vocDeployment  = Label{Attr: "k8s.deployment.name", Promote: true, Index: true}
	vocStatefulSet = Label{Attr: "k8s.statefulset.name", Promote: true, Index: true}
	vocDaemonSet   = Label{Attr: "k8s.daemonset.name", Promote: true, Index: true}
	vocJob         = Label{Attr: "k8s.job.name", Promote: true, Index: true}
	vocCronJob     = Label{Attr: "k8s.cronjob.name", Promote: true, Index: true}
	vocVolumeClaim = Label{Attr: "k8s.persistentvolumeclaim.name", Promote: true}
	vocCluster     = Label{Attr: "ikhnos.cluster.id", Promote: true, Index: true, By: "agent"}
	vocOrg         = Label{Attr: "ikhnos.org.id", Promote: true, Index: true, By: "agent"}
)

// Vocabulary is every attribute the pipeline carries.
var Vocabulary = []Label{
	vocService, vocInstance, vocNamespace, vocPod, vocNode, vocDeployment, vocStatefulSet, vocDaemonSet, vocJob, vocCronJob,
	vocVolumeClaim, vocCluster,
	{Attr: "service.namespace", Promote: true, Index: true},
	{Attr: "k8s.cluster.name", Promote: true, Index: true},
	{Attr: "k8s.replicaset.name", Promote: true, Index: true},
	{Attr: "k8s.container.name", Promote: true, Index: true},
	{Attr: "k8s.hpa.name", Promote: true},
	{Attr: "k8s.resourcequota.name", Promote: true},
	{Attr: "k8s.volume.name", Promote: true},
	vocOrg,
	{Attr: "ikhnos.source", Promote: true, By: "extractor"},
	{Attr: "ikhnos.intent.id", By: "agent"},
	{Attr: "ikhnos.scope", By: "agent"},
	{Attr: "ikhnos.operator.id", Promote: true, By: "operator"},
	{Attr: "ikhnos.operator.name", Promote: true, By: "operator"},
	{Attr: "ikhnos.category", By: "gateway"},
}

// The names the queries are built from, derived: the attribute for TraceQL and for reading a trace's resource, the label for
// PromQL and LogQL (Loki's are the same).
var (
	attrService, attrInstance, attrNamespace, attrPod, attrNode, attrCluster = vocService.Attr, vocInstance.Attr, vocNamespace.Attr, vocPod.Attr, vocNode.Attr, vocCluster.Attr

	lblService, lblNamespace, lblPod, lblNode, lblCluster = vocService.Name(), vocNamespace.Name(), vocPod.Name(), vocNode.Name(), vocCluster.Name()
	lblVolumeClaim                                        = vocVolumeClaim.Name()
	// lblWorkloads name the workload a pod belongs to. A pod's metrics carry no service name, only these (k8sattributes puts
	// them on every pod's records: agent.telemetryK8sAttrsMetadata in the agent chart). A CronJob's pods carry both its own
	// name (k8s_cronjob_name) and that of the Job it made for the run (k8s_job_name, a name generated anew each run).
	lblWorkloads = []string{vocDeployment.Name(), vocStatefulSet.Name(), vocDaemonSet.Name(), vocJob.Name(),
		vocCronJob.Name()}
	// workloadLabel is the label that names a workload of this kind in the metrics the cluster's own objects report. A
	// member is matched under its own kind's label only: a CronJob by its own name, never by its runs' Jobs.
	workloadLabel = map[string]string{"Deployment": vocDeployment.Name(), "StatefulSet": vocStatefulSet.Name(),
		"DaemonSet": vocDaemonSet.Name(), "Job": vocJob.Name(), "CronJob": vocCronJob.Name()}
)
