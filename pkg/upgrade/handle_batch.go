package upgrade

import (
	"context"

	upgradeapi "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io"
	upgradejob "github.com/rancher/system-upgrade-controller/pkg/upgrade/job"
	batchv1 "k8s.io/api/batch/v1"
)

// job events (successful completions) cause the node the job ran on to be labeled as per the plan
func (ctl *Controller) handleJobs(ctx context.Context) error {
	plans := ctl.upgradeFactory.Upgrade().V1().Plan()
	nodes := ctl.coreFactory.Core().V1().Node()

	ctl.batchFactory.Batch().V1().Job().OnChange(ctx, ctl.Name, func(key string, obj *batchv1.Job) (*batchv1.Job, error) {
		if obj == nil {
			return obj, nil
		}
		if obj.Labels != nil {
			if planName, ok := obj.Labels[upgradeapi.LabelPlan]; ok {
				if upgradejob.ConditionComplete.IsTrue(obj) {
					defer plans.Enqueue(obj.Namespace, planName)
					if planHash, ok := obj.Labels[upgradeapi.LabelHash]; ok {
						if nodeName, ok := obj.Labels[upgradeapi.LabelNode]; ok {
							node, err := nodes.Cache().Get(nodeName)
							if err != nil {
								return obj, err
							}
							node.Labels[upgradeapi.LabelPlanHash(planName)] = planHash
							if cordon, ok := obj.Labels[upgradeapi.LabelCordon]; ok && cordon == "true" {
								node.Spec.Unschedulable = false
							}
							if node, err = nodes.Update(node); err != nil {
								return obj, err
							}
						}
					}
				}
			}
		}
		return obj, nil
	})

	return nil
}
