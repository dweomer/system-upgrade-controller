package upgrade

import "path"

const (
	// LabelController is the name of the upgrade controller.
	LabelController = GroupName + `/controller`

	// LabelNode is the node being upgraded.
	LabelNode = GroupName + `/node`

	// LabelPlan is the plan being applied.
	LabelPlan = GroupName + `/plan`

	// LabelVersion is the version of the plan being applied.
	LabelVersion = GroupName + `/version`

	// LabelHash is the hash of the plan being applied.
	LabelHash = GroupName + `/hash`

	// LabelCordon is true if the plan has cordon as true or a non-nil drain spec.
	LabelCordon = GroupName + `/cordon`

	// LabelPlanSuffix is used for composing labels specific to a plan.
	LabelPlanSuffix = `plan.` + GroupName
)

func LabelPlanHash(name string) string {
	return path.Join(LabelPlanSuffix, name)
}
