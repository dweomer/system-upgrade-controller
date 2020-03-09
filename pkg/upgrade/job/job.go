package job

import (
	"os"
	"strconv"

	upgradeapi "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io"
	upgradeapiv1 "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io/v1"
	upgradectr "github.com/rancher/system-upgrade-controller/pkg/upgrade/container"
	"github.com/rancher/wrangler/pkg/condition"
	"github.com/rancher/wrangler/pkg/name"
	"github.com/sirupsen/logrus"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	defaultBackoffLimit          = int32(2)
	defaultActiveDeadlineSeconds = int64(600)
	defaultPrivileged            = true
	defaultKubectlImage          = "rancher/kubectl:1.17.0"
	defaultImagePullPolicy       = corev1.PullIfNotPresent
)

var (
	ActiveDeadlineSeconds = func(defaultValue int64) int64 {
		if str, ok := os.LookupEnv("SYSTEM_UPGRADE_JOB_ACTIVE_DEADLINE_SECONDS"); ok {
			if i, err := strconv.ParseInt(str, 10, 64); err != nil {
				logrus.Errorf("failed to parse $%s: %v", "SYSTEM_UPGRADE_JOB_ACTIVE_DEADLINE_SECONDS", err)
			} else {
				return i
			}
		}
		return defaultValue
	}(defaultActiveDeadlineSeconds)

	BackoffLimit = func(defaultValue int32) int32 {
		if str, ok := os.LookupEnv("SYSTEM_UPGRADE_JOB_BACKOFF_LIMIT"); ok {
			if i, err := strconv.ParseInt(str, 10, 32); err != nil {
				logrus.Errorf("failed to parse $%s: %v", "SYSTEM_UPGRADE_JOB_BACKOFF_LIMIT", err)
			} else {
				return int32(i)
			}
		}
		return defaultValue
	}(defaultBackoffLimit)

	KubectlImage = func(defaultValue string) string {
		if str := os.Getenv("SYSTEM_UPGRADE_JOB_KUBECTL_IMAGE"); str != "" {
			return str
		}
		return defaultValue
	}(defaultKubectlImage)

	Privileged = func(defaultValue bool) bool {
		if str, ok := os.LookupEnv("SYSTEM_UPGRADE_JOB_PRIVILEGED"); ok {
			if b, err := strconv.ParseBool(str); err != nil {
				logrus.Errorf("failed to parse $%s: %v", "SYSTEM_UPGRADE_JOB_PRIVILEGED", err)
			} else {
				return b
			}
		}
		return defaultValue
	}(defaultPrivileged)

	ImagePullPolicy = func(defaultValue corev1.PullPolicy) corev1.PullPolicy {
		if str := os.Getenv("SYSTEM_UPGRADE_JOB_IMAGE_PULL_POLICY"); str != "" {
			return corev1.PullPolicy(str)
		}
		return defaultValue
	}(defaultImagePullPolicy)
)

var (
	ConditionComplete = condition.Cond(batchv1.JobComplete)
	ConditionFailed   = condition.Cond(batchv1.JobFailed)
)

func New(plan *upgradeapiv1.Plan, nodeName, controllerName string) *batchv1.Job {
	hostPathDirectory := corev1.HostPathDirectory
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name.SafeConcatName("apply", plan.Name, "on", nodeName, "with", plan.Status.LatestHash),
			Namespace: plan.Namespace,
			Labels: labels.Set{
				upgradeapi.LabelController: controllerName,
				upgradeapi.LabelNode:       nodeName,
				upgradeapi.LabelPlan:       plan.Name,
				upgradeapi.LabelVersion:    plan.Status.LatestVersion,
				upgradeapi.LabelHash:       plan.Status.LatestHash,
				upgradeapi.LabelCordon:     strconv.FormatBool(plan.Spec.Cordon || plan.Spec.Drain != nil),
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &BackoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels.Set{
						upgradeapi.LabelController: controllerName,
						upgradeapi.LabelNode:       nodeName,
						upgradeapi.LabelPlan:       plan.Name,
						upgradeapi.LabelVersion:    plan.Status.LatestVersion,
						upgradeapi.LabelHash:       plan.Status.LatestHash,
						upgradeapi.LabelCordon:     strconv.FormatBool(plan.Spec.Cordon || plan.Spec.Drain != nil),
					},
				},
				Spec: corev1.PodSpec{
					HostIPC:            true,
					HostPID:            true,
					HostNetwork:        true,
					ServiceAccountName: plan.Spec.ServiceAccountName,
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{{
									MatchExpressions: []corev1.NodeSelectorRequirement{{
										Key:      corev1.LabelHostname,
										Operator: corev1.NodeSelectorOpIn,
										Values: []string{
											nodeName,
										},
									}},
								}},
							},
						},
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{
								LabelSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{{
										Key:      upgradeapi.LabelPlan,
										Operator: metav1.LabelSelectorOpIn,
										Values: []string{
											plan.Name,
										},
									}},
								},
								TopologyKey: corev1.LabelHostname,
							}},
						},
					},
					Tolerations: []corev1.Toleration{{
						Key:      corev1.TaintNodeUnschedulable,
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					}},
					RestartPolicy: corev1.RestartPolicyNever,
					Volumes: []corev1.Volume{{
						Name: `host-root`,
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: "/", Type: &hostPathDirectory,
							},
						},
					}, {
						Name: "pod-info",
						VolumeSource: corev1.VolumeSource{
							DownwardAPI: &corev1.DownwardAPIVolumeSource{
								Items: []corev1.DownwardAPIVolumeFile{{
									Path: "labels", FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.labels"},
								}, {
									Path: "annotations", FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.annotations"},
								}},
							},
						},
					}},
				},
			},
		},
	}
	podTemplate := &job.Spec.Template
	// setup secrets volumes
	for _, secret := range plan.Spec.Secrets {
		podTemplate.Spec.Volumes = append(podTemplate.Spec.Volumes, corev1.Volume{
			Name: name.SafeConcatName("secret", secret.Name),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secret.Name,
				},
			},
		})
	}

	// first, we prepare
	if plan.Spec.Prepare != nil {
		podTemplate.Spec.InitContainers = append(podTemplate.Spec.InitContainers,
			upgradectr.New("prepare", *plan.Spec.Prepare,
				upgradectr.WithSecrets(plan.Spec.Secrets),
				upgradectr.WithPlanEnvironment(plan.Name, plan.Status),
				upgradectr.WithImagePullPolicy(ImagePullPolicy),
			),
		)
	}

	// then we cordon/drain
	cordon, drain := plan.Spec.Cordon, plan.Spec.Drain
	if drain != nil {
		args := []string{"drain", nodeName, "--pod-selector", `!` + upgradeapi.LabelController}
		if drain.IgnoreDaemonSets == nil || *plan.Spec.Drain.IgnoreDaemonSets {
			args = append(args, "--ignore-daemonsets")
		}
		if drain.DeleteLocalData == nil || *drain.DeleteLocalData {
			args = append(args, "--delete-local-data")
		}
		if drain.Force {
			args = append(args, "--force")
		}
		if drain.Timeout != nil {
			args = append(args, "--timeout", drain.Timeout.String())
		}
		if drain.GracePeriod != nil {
			args = append(args, "--grace-period", strconv.FormatInt(int64(*drain.GracePeriod), 10))
		}
		podTemplate.Spec.InitContainers = append(podTemplate.Spec.InitContainers,
			upgradectr.New("drain", upgradeapiv1.ContainerSpec{
				Image: KubectlImage,
				Args:  args,
			},
				upgradectr.WithSecrets(plan.Spec.Secrets),
				upgradectr.WithPlanEnvironment(plan.Name, plan.Status),
				upgradectr.WithImagePullPolicy(ImagePullPolicy),
			),
		)
	} else if cordon {
		podTemplate.Spec.InitContainers = append(podTemplate.Spec.InitContainers,
			upgradectr.New("cordon", upgradeapiv1.ContainerSpec{
				Image: KubectlImage,
				Args:  []string{"cordon", nodeName},
			},
				upgradectr.WithSecrets(plan.Spec.Secrets),
				upgradectr.WithPlanEnvironment(plan.Name, plan.Status),
				upgradectr.WithImagePullPolicy(ImagePullPolicy),
			),
		)
	}

	// and finally, we upgrade
	podTemplate.Spec.Containers = []corev1.Container{
		upgradectr.New("upgrade", *plan.Spec.Upgrade,
			upgradectr.WithImageTag(plan.Status.LatestVersion),
			upgradectr.WithSecurityContext(&corev1.SecurityContext{
				Privileged: &Privileged,
				Capabilities: &corev1.Capabilities{
					Add: []corev1.Capability{
						corev1.Capability("CAP_SYS_BOOT"),
					},
				},
			}),
			upgradectr.WithSecrets(plan.Spec.Secrets),
			upgradectr.WithPlanEnvironment(plan.Name, plan.Status),
			upgradectr.WithImagePullPolicy(ImagePullPolicy),
		),
	}

	if ActiveDeadlineSeconds > 0 {
		job.Spec.ActiveDeadlineSeconds = &ActiveDeadlineSeconds
		if drain != nil && drain.Timeout != nil && drain.Timeout.Milliseconds() > ActiveDeadlineSeconds*1000 {
			logrus.Warnf("drain timeout exceeds active deadline seconds")
		}
	}

	return job
}
