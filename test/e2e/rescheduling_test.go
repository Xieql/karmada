package e2e

import (
	"fmt"
	"os"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"

	clusterv1alpha1 "github.com/karmada-io/karmada/pkg/apis/cluster/v1alpha1"
	policyv1alpha1 "github.com/karmada-io/karmada/pkg/apis/policy/v1alpha1"
	workv1alpha2 "github.com/karmada-io/karmada/pkg/apis/work/v1alpha2"
	"github.com/karmada-io/karmada/pkg/karmadactl"
	"github.com/karmada-io/karmada/pkg/karmadactl/options"
	"github.com/karmada-io/karmada/test/e2e/framework"
	testhelper "github.com/karmada-io/karmada/test/helper"
)

// reschedule testing is used to test the rescheduling situation when some initially scheduled clusters are unjoined
var _ = ginkgo.Describe("[cluster unjoined] reschedule testing", func() {
	framework.SerialContext("Deployment propagation testing", ginkgo.Label(NeedCreateCluster), func() {
		var newClusterName string
		var homeDir string
		var kubeConfigPath string
		var controlPlane string
		var clusterContext string

		ginkgo.BeforeEach(func() {
			newClusterName = "member-e2e-" + rand.String(3)
			homeDir = os.Getenv("HOME")
			kubeConfigPath = fmt.Sprintf("%s/.kube/%s.config", homeDir, newClusterName)
			controlPlane = fmt.Sprintf("%s-control-plane", newClusterName)
			clusterContext = fmt.Sprintf("kind-%s", newClusterName)
		})

		ginkgo.BeforeEach(func() {
			ginkgo.By(fmt.Sprintf("Creating cluster: %s", newClusterName), func() {
				err := createCluster(newClusterName, kubeConfigPath, controlPlane, clusterContext)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
			})
		})

		ginkgo.AfterEach(func() {
			ginkgo.By(fmt.Sprintf("Deleting clusters: %s", newClusterName), func() {
				err := deleteCluster(newClusterName, kubeConfigPath)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				_ = os.Remove(kubeConfigPath)
			})
		})

		var policyNamespace, policyName string
		var deploymentNamespace, deploymentName string
		var deployment *appsv1.Deployment
		var policy *policyv1alpha1.PropagationPolicy

		ginkgo.BeforeEach(func() {
			policyNamespace = testNamespace
			policyName = deploymentNamePrefix + rand.String(RandomStrLength)
			deploymentNamespace = testNamespace
			deploymentName = policyName
			deployment = testhelper.NewDeployment(deploymentNamespace, deploymentName)
			deployment.Spec.Replicas = pointer.Int32Ptr(10)

			policy = testhelper.NewPropagationPolicy(policyNamespace, policyName, []policyv1alpha1.ResourceSelector{
				{
					APIVersion: deployment.APIVersion,
					Kind:       deployment.Kind,
					Name:       deployment.Name,
				},
			}, policyv1alpha1.Placement{
				ReplicaScheduling: &policyv1alpha1.ReplicaSchedulingStrategy{
					ReplicaSchedulingType:     policyv1alpha1.ReplicaSchedulingTypeDivided,
					ReplicaDivisionPreference: policyv1alpha1.ReplicaDivisionPreferenceWeighted,
				},
			})
		})

		ginkgo.It("deployment reschedule testing", func() {
			ginkgo.By(fmt.Sprintf("Joinning cluster: %s", newClusterName), func() {
				karmadaConfig := karmadactl.NewKarmadaConfig(clientcmd.NewDefaultPathOptions())
				opts := karmadactl.CommandJoinOption{
					GlobalCommandOptions: options.GlobalCommandOptions{
						KarmadaContext: karmadaContext,
					},
					DryRun:            false,
					ClusterNamespace:  "karmada-cluster",
					ClusterName:       newClusterName,
					ClusterContext:    clusterContext,
					ClusterKubeConfig: kubeConfigPath,
				}
				err := karmadactl.RunJoin(karmadaConfig, opts)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				// wait for the current cluster status changing to true
				framework.WaitClusterFitWith(controlPlaneClient, newClusterName, func(cluster *clusterv1alpha1.Cluster) bool {
					return meta.IsStatusConditionPresentAndEqual(cluster.Status.Conditions, clusterv1alpha1.ClusterConditionReady, metav1.ConditionTrue)
				})
			})

			framework.CreatePropagationPolicy(karmadaClient, policy)
			framework.CreateDeployment(kubeClient, deployment)
			ginkgo.DeferCleanup(func() {
				framework.RemoveDeployment(kubeClient, deployment.Namespace, deployment.Name)
				framework.RemovePropagationPolicy(karmadaClient, policy.Namespace, policy.Name)
			})

			targetClusterNames := framework.ExtractTargetClustersFrom(controlPlaneClient, deployment)

			ginkgo.By("unjoin target cluster", func() {
				klog.Infof("Unjoining cluster %q.", newClusterName)
				karmadaConfig := karmadactl.NewKarmadaConfig(clientcmd.NewDefaultPathOptions())
				opts := karmadactl.CommandUnjoinOption{
					GlobalCommandOptions: options.GlobalCommandOptions{
						KubeConfig:     fmt.Sprintf("%s/.kube/karmada.config", os.Getenv("HOME")),
						KarmadaContext: "karmada-apiserver",
					},
					ClusterNamespace: "karmada-cluster",
					ClusterName:      newClusterName,
				}
				err := karmadactl.RunUnjoin(karmadaConfig, opts)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
			})

			ginkgo.By("check whether the deployment is rescheduled to other available clusters", func() {
				gomega.Eventually(func(g gomega.Gomega) bool {
					targetClusterNames = framework.ExtractTargetClustersFrom(controlPlaneClient, deployment)
					return testhelper.IsExclude(newClusterName, targetClusterNames)
				}, pollTimeout, pollInterval).Should(gomega.BeTrue())
			})

			ginkgo.By("check if the scheduled condition is true", func() {
				gomega.Eventually(func(g gomega.Gomega) (bool, error) {
					rb, err := getResourceBinding(deployment)
					g.Expect(err).ShouldNot(gomega.HaveOccurred())
					return meta.IsStatusConditionTrue(rb.Status.Conditions, workv1alpha2.Scheduled), nil
				}, pollTimeout, pollInterval).Should(gomega.Equal(true))
			})
		})
	})
})

// reschedule testing is used to test the rescheduling situation when some clusters are joined and recovered
var _ = ginkgo.Describe("[cluster joined] reschedule testing", func() {
	framework.SerialContext("Deployment propagation testing", ginkgo.Label(NeedCreateCluster), func() {
		var newClusterName string
		var homeDir string
		var kubeConfigPath string
		var controlPlane string
		var clusterContext string
		var initClusterNames []string

		ginkgo.BeforeEach(func() {
			newClusterName = "member-e2e-" + rand.String(3)
			homeDir = os.Getenv("HOME")
			kubeConfigPath = fmt.Sprintf("%s/.kube/%s.config", homeDir, newClusterName)
			controlPlane = fmt.Sprintf("%s-control-plane", newClusterName)
			clusterContext = fmt.Sprintf("kind-%s", newClusterName)
		})

		ginkgo.BeforeEach(func() {
			ginkgo.By(fmt.Sprintf("Creating cluster: %s", newClusterName), func() {
				err := createCluster(newClusterName, kubeConfigPath, controlPlane, clusterContext)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
			})
		})

		ginkgo.AfterEach(func() {
			ginkgo.By(fmt.Sprintf("Unjoin clsters: %s", newClusterName), func() {
				karmadaConfig := karmadactl.NewKarmadaConfig(clientcmd.NewDefaultPathOptions())
				opts := karmadactl.CommandUnjoinOption{
					GlobalCommandOptions: options.GlobalCommandOptions{
						KubeConfig:     fmt.Sprintf("%s/.kube/karmada.config", os.Getenv("HOME")),
						KarmadaContext: "karmada-apiserver",
					},
					ClusterNamespace: "karmada-cluster",
					ClusterName:      newClusterName,
				}
				err := karmadactl.RunUnjoin(karmadaConfig, opts)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
			})
			ginkgo.By(fmt.Sprintf("Deleting clusters: %s", newClusterName), func() {
				err := deleteCluster(newClusterName, kubeConfigPath)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				_ = os.Remove(kubeConfigPath)
			})
		})

		var policyNamespace, policyName string
		var deploymentNamespace, deploymentName string
		var deployment *appsv1.Deployment
		var policy *policyv1alpha1.PropagationPolicy
		ginkgo.Context("testing the ReplicaSchedulingType of the policy is Duplicated", func() {
			ginkgo.BeforeEach(func() {
				policyNamespace = testNamespace
				policyName = deploymentNamePrefix + rand.String(RandomStrLength)
				deploymentNamespace = testNamespace
				deploymentName = policyName
				deployment = testhelper.NewDeployment(deploymentNamespace, deploymentName)
				deployment.Spec.Replicas = pointer.Int32Ptr(1)
				// set ReplicaSchedulingType=Duplicated.
				policy = testhelper.NewPropagationPolicy(policyNamespace, policyName, []policyv1alpha1.ResourceSelector{
					{
						APIVersion: deployment.APIVersion,
						Kind:       deployment.Kind,
						Name:       deployment.Name,
					},
				}, policyv1alpha1.Placement{
					ReplicaScheduling: &policyv1alpha1.ReplicaSchedulingStrategy{
						ReplicaSchedulingType: policyv1alpha1.ReplicaSchedulingTypeDuplicated,
					},
				})
			})

			ginkgo.It("when the ReplicaSchedulingType of the policy is Duplicated, reschedule testing", func() {
				ginkgo.By("create deployment and policy")
				framework.CreatePropagationPolicy(karmadaClient, policy)
				framework.CreateDeployment(kubeClient, deployment)
				ginkgo.DeferCleanup(func() {
					framework.RemoveDeployment(kubeClient, deployment.Namespace, deployment.Name)
					framework.RemovePropagationPolicy(karmadaClient, policy.Namespace, policy.Name)
				})

				ginkgo.By(fmt.Sprintf("Joinning cluster: %s", newClusterName))
				karmadaConfig := karmadactl.NewKarmadaConfig(clientcmd.NewDefaultPathOptions())
				opts := karmadactl.CommandJoinOption{
					GlobalCommandOptions: options.GlobalCommandOptions{
						KarmadaContext: karmadaContext,
					},
					DryRun:            false,
					ClusterNamespace:  "karmada-cluster",
					ClusterName:       newClusterName,
					ClusterContext:    clusterContext,
					ClusterKubeConfig: kubeConfigPath,
				}
				err := karmadactl.RunJoin(karmadaConfig, opts)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				// wait for the current cluster status changing to true
				framework.WaitClusterFitWith(controlPlaneClient, newClusterName, func(cluster *clusterv1alpha1.Cluster) bool {
					return meta.IsStatusConditionPresentAndEqual(cluster.Status.Conditions, clusterv1alpha1.ClusterConditionReady, metav1.ConditionTrue)
				})

				ginkgo.By("check whether the deployment is rescheduled to a new cluster")
				gomega.Eventually(func(g gomega.Gomega) bool {
					targetClusterNames := framework.ExtractTargetClustersFrom(controlPlaneClient, deployment)
					return !testhelper.IsExclude(newClusterName, targetClusterNames)
				}, pollTimeout, pollInterval).Should(gomega.BeTrue())
			})
		})

		ginkgo.Context("testing clusterAffinity of the policy", func() {
			ginkgo.BeforeEach(func() {
				initClusterNames = []string{"member1", "member2", newClusterName}
				policyNamespace = testNamespace
				policyName = deploymentNamePrefix + rand.String(RandomStrLength)
				deploymentNamespace = testNamespace
				deploymentName = policyName
				deployment = testhelper.NewDeployment(deploymentNamespace, deploymentName)
				deployment.Spec.Replicas = pointer.Int32Ptr(1)
				// set clusterAffinity for Placement.
				policy = testhelper.NewPropagationPolicy(policyNamespace, policyName, []policyv1alpha1.ResourceSelector{
					{
						APIVersion: deployment.APIVersion,
						Kind:       deployment.Kind,
						Name:       deployment.Name,
					},
				}, policyv1alpha1.Placement{
					ClusterAffinity: &policyv1alpha1.ClusterAffinity{ClusterNames: initClusterNames},
				})
			})
			ginkgo.It("when the ReplicaScheduling of the policy is nil, reschedule testing", func() {
				ginkgo.By("create deployment and policy")

				framework.CreatePropagationPolicy(karmadaClient, policy)

				framework.CreateDeployment(kubeClient, deployment)
				ginkgo.DeferCleanup(func() {
					framework.RemoveDeployment(kubeClient, deployment.Namespace, deployment.Name)
					framework.RemovePropagationPolicy(karmadaClient, policy.Namespace, policy.Name)
				})
				gomega.Eventually(func(g gomega.Gomega) bool {
					targetClusterNames := framework.ExtractTargetClustersFrom(controlPlaneClient, deployment)
					return testhelper.IsExclude(newClusterName, targetClusterNames)
				}, pollTimeout, pollInterval).Should(gomega.BeTrue())

				ginkgo.By(fmt.Sprintf("Joinning cluster: %s", newClusterName))
				karmadaConfig := karmadactl.NewKarmadaConfig(clientcmd.NewDefaultPathOptions())
				opts := karmadactl.CommandJoinOption{
					GlobalCommandOptions: options.GlobalCommandOptions{
						KarmadaContext: karmadaContext,
					},
					DryRun:            false,
					ClusterNamespace:  "karmada-cluster",
					ClusterName:       newClusterName,
					ClusterContext:    clusterContext,
					ClusterKubeConfig: kubeConfigPath,
				}
				err := karmadactl.RunJoin(karmadaConfig, opts)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				// wait for the current cluster status changing to true
				framework.WaitClusterFitWith(controlPlaneClient, newClusterName, func(cluster *clusterv1alpha1.Cluster) bool {
					return meta.IsStatusConditionPresentAndEqual(cluster.Status.Conditions, clusterv1alpha1.ClusterConditionReady, metav1.ConditionTrue)
				})

				ginkgo.By("check whether the deployment is rescheduled to a new cluster")
				gomega.Eventually(func(g gomega.Gomega) bool {
					targetClusterNames := framework.ExtractTargetClustersFrom(controlPlaneClient, deployment)
					for _, clusterName := range initClusterNames {
						if testhelper.IsExclude(clusterName, targetClusterNames) {
							return false
						}
					}
					return true
				}, pollTimeout, pollInterval).Should(gomega.BeTrue())

				gomega.Eventually(func(g gomega.Gomega) bool {
					targetClusterNames := framework.ExtractTargetClustersFrom(controlPlaneClient, deployment)
					return testhelper.IsExclude("member3", targetClusterNames)
				}, pollTimeout, pollInterval).Should(gomega.BeTrue())

			})
		})
	})
})
