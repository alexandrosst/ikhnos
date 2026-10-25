package operatorsapi

import (
	"net/http"

	"ikhnos/internal/install"
	"ikhnos/internal/kernel"
)

// controllerBlock is the telemetry of a command to an operator written as the patch of an IkhnosAgent resource, for an agent a controller
// keeps. The controller puts back whatever a helm upgrade changes (the release is kept to the resource), so for such an agent the resource
// is where the destination has to be: the Secrets are made as for helm, and the patch states each operator as a destination reached over
// mutual TLS with the Secret that holds its certificate. Anything a resource cannot state is told as a problem, and the helm block stands.
func (h *API) controllerBlock(_ *http.Request, img kernel.ImageConfig, spec install.Telemetry, reach map[string]install.OperatorTLS, namespace string, tier int, secretCommands []string) *intentControllerDoc {
	doc := &intentControllerDoc{Problems: []string{}}
	rt, err := install.ResourceOfOperators(spec, reach)
	if err != nil {
		if pe, ok := err.(*install.ProblemsError); ok {
			doc.Problems = append(doc.Problems, pe.Problems...)
		} else {
			doc.Problems = append(doc.Problems, err.Error())
		}
		return doc
	}
	if rt == nil {
		doc.Problems = append(doc.Problems, "these settings turn telemetry off, which is not a change to an operator's destination")
		return doc
	}
	measurements := spec.MeasurementsOn == nil || *spec.MeasurementsOn
	if p := rt.Problems(tier, measurements); len(p) > 0 {
		doc.Problems = append(doc.Problems, p...)
		return doc
	}
	patch, err := install.ResourcePatch{Namespace: namespace, SetTelemetry: true, Telemetry: rt}.Render()
	if err != nil {
		doc.Problems = append(doc.Problems, err.Error())
		return doc
	}
	doc.Patch = patch
	first := []string{}
	// Its definition first: an old one in the cluster drops the destination from the resource without a word (Helm never upgrades it).
	if ref, version := h.Settings.ControllerChart(img); ref != "" {
		if cmd, err := (install.ControllerInstall{Chart: ref, Version: version}).Definition(); err == nil {
			first = append(first, cmd)
		}
	}
	doc.Command = install.Chain(append(append(first, secretCommands...), patch))
	doc.NeedsTelemetryPermission = true
	if ref, version := h.Settings.ControllerChart(img); ref != "" {
		ctl := install.ControllerInstall{Chart: ref, Version: version}
		if cmd, err := ctl.AllowTelemetry(); err == nil {
			doc.AllowTelemetry = ctl.WithDefinition(cmd)
		}
	}
	return doc
}
