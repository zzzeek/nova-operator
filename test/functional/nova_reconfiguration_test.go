/*
Copyright 2022.

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
package functional_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/openstack-k8s-operators/lib-common/modules/common/test/helpers"

	condition "github.com/openstack-k8s-operators/lib-common/modules/common/condition"
	novav1 "github.com/openstack-k8s-operators/nova-operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

func CreateNovaWith3CellsAndEnsureReady(novaNames NovaNames) {
	cell0 := novaNames.Cells["cell0"]
	cell1 := novaNames.Cells["cell1"]
	cell2 := novaNames.Cells["cell2"]

	DeferCleanup(k8sClient.Delete, ctx, CreateNovaSecret(novaNames.NovaName.Namespace, SecretName))
	DeferCleanup(k8sClient.Delete, ctx, CreateNovaMessageBusSecret(cell0))
	DeferCleanup(k8sClient.Delete, ctx, CreateNovaMessageBusSecret(cell1))
	DeferCleanup(k8sClient.Delete, ctx, CreateNovaMessageBusSecret(cell2))

	serviceSpec := corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 3306}}}
	DeferCleanup(
		mariadb.DeleteDBService,
		mariadb.CreateDBService(novaNames.APIMariaDBDatabaseName.Namespace, novaNames.APIMariaDBDatabaseName.Name, serviceSpec))
	DeferCleanup(mariadb.DeleteDBService, mariadb.CreateDBService(cell0.MariaDBDatabaseName.Namespace, cell0.MariaDBDatabaseName.Name, serviceSpec))
	DeferCleanup(mariadb.DeleteDBService, mariadb.CreateDBService(cell1.MariaDBDatabaseName.Namespace, cell1.MariaDBDatabaseName.Name, serviceSpec))
	DeferCleanup(mariadb.DeleteDBService, mariadb.CreateDBService(cell2.MariaDBDatabaseName.Namespace, cell2.MariaDBDatabaseName.Name, serviceSpec))

	spec := GetDefaultNovaSpec()
	cell0Template := GetDefaultNovaCellTemplate()
	cell0Template["cellDatabaseInstance"] = cell0.MariaDBDatabaseName.Name
	cell0Template["cellDatabaseUser"] = "nova_cell0"

	cell1Template := GetDefaultNovaCellTemplate()
	cell1Template["cellDatabaseInstance"] = cell1.MariaDBDatabaseName.Name
	cell1Template["cellDatabaseUser"] = "nova_cell1"
	cell1Template["cellMessageBusInstance"] = cell1.TransportURLName.Name
	cell1Template["novaComputeTemplates"] = map[string]interface{}{
		ironicComputeName: GetDefaultNovaComputeTemplate(),
	}

	cell2Template := GetDefaultNovaCellTemplate()
	cell2Template["cellDatabaseInstance"] = cell2.MariaDBDatabaseName.Name
	cell2Template["cellDatabaseUser"] = "nova_cell2"
	cell2Template["cellMessageBusInstance"] = cell2.TransportURLName.Name
	cell2Template["hasAPIAccess"] = false

	spec["cellTemplates"] = map[string]interface{}{
		"cell0": cell0Template,
		"cell1": cell1Template,
		"cell2": cell2Template,
	}
	spec["apiDatabaseInstance"] = novaNames.APIMariaDBDatabaseName.Name
	spec["apiMessageBusInstance"] = cell0.TransportURLName.Name

	DeferCleanup(th.DeleteInstance, CreateNova(novaNames.NovaName, spec))
	DeferCleanup(keystone.DeleteKeystoneAPI, keystone.CreateKeystoneAPI(novaNames.NovaName.Namespace))
	keystone.SimulateKeystoneServiceReady(novaNames.KeystoneServiceName)
	// END of common logic with Nova multicell test

	mariadb.SimulateMariaDBDatabaseCompleted(novaNames.APIMariaDBDatabaseName)
	mariadb.SimulateMariaDBAccountCompleted(novaNames.APIMariaDBDatabaseName)
	mariadb.SimulateMariaDBDatabaseCompleted(cell0.MariaDBDatabaseName)
	mariadb.SimulateMariaDBAccountCompleted(cell0.MariaDBDatabaseName)
	mariadb.SimulateMariaDBDatabaseCompleted(cell1.MariaDBDatabaseName)
	mariadb.SimulateMariaDBAccountCompleted(cell1.MariaDBDatabaseName)
	mariadb.SimulateMariaDBDatabaseCompleted(cell2.MariaDBDatabaseName)
	mariadb.SimulateMariaDBAccountCompleted(cell2.MariaDBDatabaseName)

	infra.SimulateTransportURLReady(cell0.TransportURLName)
	infra.SimulateTransportURLReady(cell1.TransportURLName)
	infra.SimulateTransportURLReady(cell2.TransportURLName)

	th.SimulateJobSuccess(cell0.DBSyncJobName)
	th.SimulateStatefulSetReplicaReady(cell0.ConductorStatefulSetName)
	th.SimulateJobSuccess(cell0.CellMappingJobName)

	th.SimulateStatefulSetReplicaReady(novaNames.APIDeploymentName)
	keystone.SimulateKeystoneEndpointReady(novaNames.APIKeystoneEndpointName)

	th.SimulateStatefulSetReplicaReady(cell1.NoVNCProxyStatefulSetName)
	th.SimulateJobSuccess(cell1.DBSyncJobName)
	th.SimulateStatefulSetReplicaReady(cell1.ConductorStatefulSetName)
	th.SimulateStatefulSetReplicaReady(cell1.NovaComputeStatefulSetName)
	th.SimulateJobSuccess(cell1.CellMappingJobName)
	th.SimulateJobSuccess(cell1.HostDiscoveryJobName)

	th.SimulateStatefulSetReplicaReady(cell2.NoVNCProxyStatefulSetName)
	th.SimulateJobSuccess(cell2.DBSyncJobName)
	th.SimulateStatefulSetReplicaReady(cell2.ConductorStatefulSetName)
	th.SimulateJobSuccess(cell2.CellMappingJobName)
	th.SimulateStatefulSetReplicaReady(novaNames.SchedulerStatefulSetName)
	th.SimulateStatefulSetReplicaReady(novaNames.MetadataStatefulSetName)
	th.ExpectCondition(
		novaNames.NovaName,
		ConditionGetterFunc(NovaConditionGetter),
		novav1.NovaAllCellsReadyCondition,
		corev1.ConditionTrue,
	)
	th.ExpectCondition(
		novaNames.NovaName,
		ConditionGetterFunc(NovaConditionGetter),
		condition.ReadyCondition,
		corev1.ConditionTrue,
	)
}

var _ = Describe("Nova reconfiguration", func() {
	BeforeEach(func() {
		// Uncomment this if you need the full output in the logs from gomega
		// matchers
		// format.MaxLength = 0

		CreateNovaWith3CellsAndEnsureReady(novaNames)
	})
	When("cell0 conductor replicas is set to 0", func() {
		It("sets the deployment replicas to 0", func() {
			cell0DeploymentName := cell0.ConductorStatefulSetName

			deployment := th.GetStatefulSet(cell0DeploymentName)
			one := int32(1)
			Expect(deployment.Spec.Replicas).To(Equal(&one))

			// We need this big Eventually block because the Update() call might
			// return a Conflict and then we have to retry by re-reading Nova,
			// and updating the Replicas again.
			Eventually(func(g Gomega) {
				nova := GetNova(novaNames.NovaName)

				// TODO(gibi): Is there a simpler way to achieve this update
				// in golang?
				zero := int32(0)
				cell0 := nova.Spec.CellTemplates["cell0"]
				(&cell0).ConductorServiceTemplate.Replicas = &zero
				nova.Spec.CellTemplates["cell0"] = cell0

				g.Expect(k8sClient.Update(ctx, nova)).To(Succeed())

				deployment = &appsv1.StatefulSet{}
				g.Expect(k8sClient.Get(ctx, cell0DeploymentName, deployment)).Should(Succeed())
				g.Expect(deployment.Spec.Replicas).To(Equal(&zero))
			}, timeout, interval).Should(Succeed())
		})
	})
	When("networkAttachemnt is added to a conductor while the definition is missing", func() {
		It("applies new NetworkAttachments configuration to that Conductor", func() {
			Eventually(func(g Gomega) {
				nova := GetNova(novaNames.NovaName)

				cell1 := nova.Spec.CellTemplates["cell1"]
				attachments := cell1.ConductorServiceTemplate.NetworkAttachments
				attachments = append(attachments, "internalapi")
				(&cell1).ConductorServiceTemplate.NetworkAttachments = attachments
				nova.Spec.CellTemplates["cell1"] = cell1

				g.Expect(k8sClient.Update(ctx, nova)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			th.ExpectConditionWithDetails(
				cell1.ConductorName,
				ConditionGetterFunc(NovaConductorConditionGetter),
				condition.NetworkAttachmentsReadyCondition,
				corev1.ConditionFalse,
				condition.RequestedReason,
				"NetworkAttachment resources missing: internalapi",
			)
			th.ExpectConditionWithDetails(
				cell1.ConductorName,
				ConditionGetterFunc(NovaConductorConditionGetter),
				condition.ReadyCondition,
				corev1.ConditionFalse,
				condition.RequestedReason,
				"NetworkAttachment resources missing: internalapi",
			)

			th.ExpectConditionWithDetails(
				cell1.CellCRName,
				ConditionGetterFunc(NovaCellConditionGetter),
				novav1.NovaConductorReadyCondition,
				corev1.ConditionFalse,
				condition.RequestedReason,
				"NetworkAttachment resources missing: internalapi",
			)
			th.ExpectConditionWithDetails(
				cell1.CellCRName,
				ConditionGetterFunc(NovaCellConditionGetter),
				condition.ReadyCondition,
				corev1.ConditionFalse,
				condition.RequestedReason,
				"NetworkAttachment resources missing: internalapi",
			)

			th.ExpectConditionWithDetails(
				novaNames.NovaName,
				ConditionGetterFunc(NovaConditionGetter),
				novav1.NovaAllCellsReadyCondition,
				corev1.ConditionFalse,
				condition.RequestedReason,
				"NovaCell cell1 is not Ready",
			)
			th.ExpectConditionWithDetails(
				novaNames.NovaName,
				ConditionGetterFunc(NovaConditionGetter),
				condition.ReadyCondition,
				corev1.ConditionFalse,
				condition.RequestedReason,
				"NovaCell cell1 is not Ready",
			)

			DeferCleanup(th.DeleteInstance, th.CreateNetworkAttachmentDefinition(novaNames.InternalAPINetworkNADName))

			th.ExpectConditionWithDetails(
				novaNames.NovaName,
				ConditionGetterFunc(NovaConditionGetter),
				condition.ReadyCondition,
				corev1.ConditionFalse,
				condition.RequestedReason,
				"NovaCell cell1 is not Ready",
			)

			th.SimulateStatefulSetReplicaReadyWithPods(
				cell1.ConductorStatefulSetName,
				map[string][]string{novaNames.NovaName.Namespace + "/internalapi": {"10.0.0.1"}},
			)

			th.ExpectCondition(
				novaNames.NovaName,
				ConditionGetterFunc(NovaConditionGetter),
				condition.ReadyCondition,
				corev1.ConditionTrue,
			)
		})
	})

	When("global NodeSelector is set", func() {
		DescribeTable("it is propagated to", func(serviceNameFunc func() types.NamespacedName) {
			// We need this big Eventually block because the Update() call might
			// return a Conflict and then we have to retry by re-reading Nova,
			// and updating it again.
			Eventually(func(g Gomega) {
				nova := GetNova(novaNames.NovaName)

				newSelector := map[string]string{"foo": "bar"}
				nova.Spec.NodeSelector = newSelector

				g.Expect(k8sClient.Update(ctx, nova)).To(Succeed())

				novaDeploymentName := serviceNameFunc()
				serviceDeployment := th.GetStatefulSet(novaDeploymentName)
				g.Expect(serviceDeployment.Spec.Template.Spec.NodeSelector).To(Equal(newSelector))

			}, timeout, interval).Should(Succeed())

			// Now reset it back to empty and see that it is propagates too
			Eventually(func(g Gomega) {
				nova := GetNova(novaNames.NovaName)

				newSelector := map[string]string{}
				nova.Spec.NodeSelector = newSelector

				g.Expect(k8sClient.Update(ctx, nova)).To(Succeed())

				serviceDeploymentName := serviceNameFunc()
				serviceDeployment := th.GetStatefulSet(serviceDeploymentName)
				g.Expect(serviceDeployment.Spec.Template.Spec.NodeSelector).To(BeNil())
			}, timeout, interval).Should(Succeed())
		},
			Entry("the nova api pods",
				func() types.NamespacedName {
					return novaNames.APIName
				}),
			Entry("the nova scheduler pods", func() types.NamespacedName {
				return novaNames.SchedulerName
			}),
			Entry("the nova metadata pods", func() types.NamespacedName {
				return novaNames.MetadataName
			}),
			Entry("the nova cell0 conductor", func() types.NamespacedName {
				return cell0.ConductorStatefulSetName
			}),
			Entry("the nova cell1 conductor", func() types.NamespacedName {
				return cell1.ConductorStatefulSetName
			}),
			Entry("the nova cell2 conductor", func() types.NamespacedName {
				return cell2.ConductorStatefulSetName
			}),
		)

		It("does not override non empty NodeSelector defined in the service template", func() {
			serviceSelector := map[string]string{"foo": "api"}
			conductorSelector := map[string]string{"foo": "conductor"}
			globalSelector := map[string]string{"foo": "global"}

			// Set the service specific NodeSelector first
			Eventually(func(g Gomega) {
				nova := GetNova(novaNames.NovaName)

				nova.Spec.APIServiceTemplate.NodeSelector = serviceSelector
				nova.Spec.MetadataServiceTemplate.NodeSelector = serviceSelector
				nova.Spec.SchedulerServiceTemplate.NodeSelector = serviceSelector
				for _, cell := range []string{"cell0", "cell1", "cell2"} {
					cellTemplate := nova.Spec.CellTemplates[cell]
					cellTemplate.ConductorServiceTemplate.NodeSelector = conductorSelector
					nova.Spec.CellTemplates[cell] = cellTemplate
				}
				g.Expect(k8sClient.Update(ctx, nova)).To(Succeed())

				apiDeployment := th.GetStatefulSet(novaNames.APIDeploymentName)
				g.Expect(apiDeployment.Spec.Template.Spec.NodeSelector).To(Equal(serviceSelector))
				schedulerDeployment := th.GetStatefulSet(novaNames.SchedulerStatefulSetName)
				g.Expect(schedulerDeployment.Spec.Template.Spec.NodeSelector).To(Equal(serviceSelector))
				metadataDeployment := th.GetStatefulSet(novaNames.MetadataStatefulSetName)
				g.Expect(metadataDeployment.Spec.Template.Spec.NodeSelector).To(Equal(serviceSelector))

				conductorDeployment := th.GetStatefulSet(cell0.ConductorStatefulSetName)
				g.Expect(conductorDeployment.Spec.Template.Spec.NodeSelector).To(Equal(conductorSelector))
				conductorDeployment = th.GetStatefulSet(cell1.ConductorStatefulSetName)
				g.Expect(conductorDeployment.Spec.Template.Spec.NodeSelector).To(Equal(conductorSelector))
				conductorDeployment = th.GetStatefulSet(cell2.ConductorStatefulSetName)
				g.Expect(conductorDeployment.Spec.Template.Spec.NodeSelector).To(Equal(conductorSelector))
			}, timeout, interval).Should(Succeed())

			// Set the global NodeSelector and assert that it is propagated
			// except to the NovaService's
			Eventually(func(g Gomega) {
				nova := GetNova(novaNames.NovaName)
				nova.Spec.NodeSelector = globalSelector

				g.Expect(k8sClient.Update(ctx, nova)).To(Succeed())

				// NovaService's deployment keeps it own selector
				apiDeployment := th.GetStatefulSet(novaNames.APIDeploymentName)
				g.Expect(apiDeployment.Spec.Template.Spec.NodeSelector).To(Equal(serviceSelector))
				schedulerDeployment := th.GetStatefulSet(novaNames.SchedulerStatefulSetName)
				g.Expect(schedulerDeployment.Spec.Template.Spec.NodeSelector).To(Equal(serviceSelector))
				metadataDeployment := th.GetStatefulSet(novaNames.MetadataStatefulSetName)
				g.Expect(metadataDeployment.Spec.Template.Spec.NodeSelector).To(Equal(serviceSelector))

				// and cell conductors keep their own selector
				conductorDeployment := th.GetStatefulSet(cell0.ConductorStatefulSetName)
				g.Expect(conductorDeployment.Spec.Template.Spec.NodeSelector).To(Equal(conductorSelector))
				conductorDeployment = th.GetStatefulSet(cell1.ConductorStatefulSetName)
				g.Expect(conductorDeployment.Spec.Template.Spec.NodeSelector).To(Equal(conductorSelector))
				conductorDeployment = th.GetStatefulSet(cell2.ConductorStatefulSetName)
				g.Expect(conductorDeployment.Spec.Template.Spec.NodeSelector).To(Equal(conductorSelector))
			}, timeout, interval).Should(Succeed())
		})
	})
	When("CellMessageBusInstance is reconfigured for a cell", func() {
		It("updates the cell, re-runs the cell mapping job and updates the cell hash", func() {
			mappingJob := th.GetJob(cell1.CellMappingJobName)
			oldJobInputHash := GetEnvVarValue(
				mappingJob.Spec.Template.Spec.Containers[0].Env, "INPUT_HASH", "")

			oldCell1Hash := GetNova(novaNames.NovaName).Status.RegisteredCells[cell1.CellCRName.Name]
			oldComputeConfigHash := GetNovaCell(cell1.CellCRName).Status.Hash[cell1.ComputeConfigSecretName.Name]

			Eventually(func(g Gomega) {
				nova := GetNova(novaNames.NovaName)

				cell1 := nova.Spec.CellTemplates["cell1"]
				cell1.CellMessageBusInstance = "alternate-mq-for-cell1"
				nova.Spec.CellTemplates["cell1"] = cell1

				g.Expect(k8sClient.Update(ctx, nova)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			// The new TransportURL will point to a new secret so we need to
			// simulate that is created by the infra-operator.
			s := th.CreateSecret(
				types.NamespacedName{Namespace: cell1.CellCRName.Namespace, Name: "alternate-mq-for-cell1-secret"},
				map[string][]byte{
					"transport_url": []byte("rabbit://alternate-mq-for-cell1/fake"),
				},
			)
			DeferCleanup(k8sClient.Delete, ctx, s)

			// Expect that nova controller updates the TransportURL to point to
			// the new rabbit cluster
			Eventually(func(g Gomega) {
				transport := infra.GetTransportURL(cell1.TransportURLName)
				g.Expect(transport.Spec.RabbitmqClusterName).To(Equal("alternate-mq-for-cell1"))
			}, timeout, interval).Should(Succeed())

			infra.SimulateTransportURLReady(cell1.TransportURLName)

			// Expect that the NovaConductor config is updated with the new transport URL
			Eventually(func(g Gomega) {
				configDataMap := th.GetSecret(cell1.ConductorConfigDataName)
				g.Expect(configDataMap).ShouldNot(BeNil())
				g.Expect(configDataMap.Data).Should(HaveKey("01-nova.conf"))
				configData := string(configDataMap.Data["01-nova.conf"])
				g.Expect(configData).Should(ContainSubstring("transport_url=rabbit://alternate-mq-for-cell1/fake"))
			}, timeout, interval).Should(Succeed())

			// Expect that nova controller updates the mapping Job to re-run that
			// to update the CellMapping table in the nova_api DB.
			Eventually(func(g Gomega) {
				mappingJob := th.GetJob(cell1.CellMappingJobName)
				newJobInputHash := GetEnvVarValue(
					mappingJob.Spec.Template.Spec.Containers[0].Env, "INPUT_HASH", "")
				g.Expect(newJobInputHash).NotTo(Equal(oldJobInputHash))
			}, timeout, interval).Should(Succeed())

			th.SimulateJobSuccess(cell1.CellMappingJobName)

			// Expect that the new config results in a new cell1 hash
			Eventually(func(g Gomega) {
				newCell1Hash := GetNova(novaNames.NovaName).Status.RegisteredCells[cell1.CellCRName.Name]
				g.Expect(newCell1Hash).NotTo(Equal(oldCell1Hash))
			}, timeout, interval).Should(Succeed())

			// Expect that the compute config is updated with the new transport URL
			Eventually(func(g Gomega) {
				configDataMap := th.GetSecret(cell1.ComputeConfigSecretName)
				g.Expect(configDataMap).ShouldNot(BeNil())
				g.Expect(configDataMap.Data).Should(HaveKey("01-nova.conf"))
				configData := string(configDataMap.Data["01-nova.conf"])
				g.Expect(configData).Should(ContainSubstring("transport_url=rabbit://alternate-mq-for-cell1/fake"))
			}, timeout, interval).Should(Succeed())
			Expect(GetNovaCell(cell1.CellCRName).Status.Hash[cell1.ComputeConfigSecretName.Name]).NotTo(Equal(oldComputeConfigHash))
		})
	})

	When("computeReplica is reconfigured for a cell", func() {
		It("updates the cell, re-runs the cell discover job", func() {
			discoverJob := th.GetJob(cell1.HostDiscoveryJobName)
			oldJobInputHash := GetEnvVarValue(
				discoverJob.Spec.Template.Spec.Containers[0].Env, "INPUT_HASH", "")

			Eventually(func(g Gomega) {
				nova := GetNova(novaNames.NovaName)
				cell1 := nova.Spec.CellTemplates["cell1"]
				ironicTemplate := cell1.NovaComputeTemplates[ironicComputeName]
				// we change replicas to rerun job.In that case replica can be only set to 0
				// because ironicDriver can't have more than 1 replica
				ironicTemplate.Replicas = ptr.To(int32(0))
				cell1.NovaComputeTemplates[ironicComputeName] = ironicTemplate
				nova.Spec.CellTemplates["cell1"] = cell1

				g.Expect(k8sClient.Update(ctx, nova)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			// Expect that nova controller updates the mapping Job to re-run that
			// to update the CellMapping table in the nova_api DB.
			Eventually(func(g Gomega) {
				discoverJob := th.GetJob(cell1.HostDiscoveryJobName)
				newJobInputHash := GetEnvVarValue(
					discoverJob.Spec.Template.Spec.Containers[0].Env, "INPUT_HASH", "")
				g.Expect(newJobInputHash).NotTo(Equal(oldJobInputHash))
			}, timeout, interval).Should(Succeed())
		})
	})

	When("the service password in the osp secret is changed", func() {
		It("reconfigures every nova service", func() {
			secretName := types.NamespacedName{Namespace: novaNames.NovaName.Namespace, Name: SecretName}
			th.UpdateSecret(secretName, "NovaPassword", []byte("new-service-password"))

			// Expect that every service config is updated with the new service password
			for _, cmName := range []types.NamespacedName{
				cell0.ConductorConfigDataName,
				cell1.ConductorConfigDataName,
				cell1.CellNoVNCProxyNameConfigDataName,
				cell1.ComputeConfigSecretName,
				cell2.ConductorConfigDataName,
				cell2.CellNoVNCProxyNameConfigDataName,
				cell2.ComputeConfigSecretName,
				novaNames.APIConfigDataName,
				novaNames.SchedulerConfigDataName,
				novaNames.MetadataConfigDataName,
			} {
				Eventually(func(g Gomega) {
					configDataMap := th.GetSecret(cmName)

					g.Expect(configDataMap.Data).Should(HaveKey("01-nova.conf"))
					conf := string(configDataMap.Data["01-nova.conf"])
					g.Expect(conf).Should(ContainSubstring(("password = new-service-password")))
					g.Expect(conf).ShouldNot(ContainSubstring(("password = 12345678")))

				}, timeout, interval).Should(Succeed(), fmt.Sprintf("Failed on %s", cmName))
			}
		})
		It("updates the hash in the statefulsets to trigger the restart with the new config", func() {
			var ssNames = []types.NamespacedName{
				cell0.ConductorStatefulSetName,
				cell1.ConductorStatefulSetName,
				cell2.ConductorStatefulSetName,
				cell1.NoVNCProxyStatefulSetName,
				cell2.NoVNCProxyStatefulSetName,
				novaNames.APIStatefulSetName,
				novaNames.SchedulerStatefulSetName,
				novaNames.MetadataStatefulSetName,
			}
			var originalHashes []string = []string{}

			// Grab the current statefulset config hashes
			for _, ss := range ssNames {
				originalHash := GetEnvVarValue(
					th.GetStatefulSet(ss).Spec.Template.Spec.Containers[0].Env, "CONFIG_HASH", "")
				Expect(originalHash).NotTo(BeEmpty())
				originalHashes = append(originalHashes, originalHash)
			}

			secretName := types.NamespacedName{Namespace: novaNames.NovaName.Namespace, Name: SecretName}
			th.UpdateSecret(secretName, "NovaPassword", []byte("new-service-password"))

			// Assert that the config hash is updated in each stateful set
			for i, ss := range ssNames {
				Eventually(func(g Gomega) {
					newHash := GetEnvVarValue(
						th.GetStatefulSet(ss).Spec.Template.Spec.Containers[0].Env, "CONFIG_HASH", "")
					g.Expect(newHash).NotTo(BeEmpty())
					g.Expect(newHash).NotTo(Equal(originalHashes[i]))
				}, timeout, interval).Should(Succeed())
			}
		})
	})
	It("deletes NovaMetadata if it is disabled", func() {
		Eventually(func(g Gomega) {
			nova := GetNova(novaNames.NovaName)
			nova.Spec.MetadataServiceTemplate.Enabled = ptr.To(false)

			g.Expect(k8sClient.Update(ctx, nova)).To(Succeed())
		}, timeout, interval).Should(Succeed())

		AssertMetadataDoesNotExist(novaNames.MetadataName)
		th.ExpectCondition(
			novaNames.NovaName,
			ConditionGetterFunc(NovaConditionGetter),
			condition.ReadyCondition,
			corev1.ConditionTrue,
		)
	})
	It("creates NovaMetadata if it is enabled", func() {
		//disable it first
		Eventually(func(g Gomega) {
			nova := GetNova(novaNames.NovaName)
			nova.Spec.MetadataServiceTemplate.Enabled = ptr.To(false)

			g.Expect(k8sClient.Update(ctx, nova)).To(Succeed())
		}, timeout, interval).Should(Succeed())

		AssertMetadataDoesNotExist(novaNames.MetadataName)
		th.ExpectCondition(
			novaNames.NovaName,
			ConditionGetterFunc(NovaConditionGetter),
			condition.ReadyCondition,
			corev1.ConditionTrue,
		)
		nova := GetNova(novaNames.NovaName)
		Expect(nova.Status.MetadataServiceReadyCount).To(Equal(int32(0)))
		// NOTE(gibi): This only needed in envtest, in a real k8s
		// deployment the garbage collector would delete the StatefulSet
		// when its parents, the NovaMetadata, is deleted, but that garbage
		// collector does not run in envtest. So we manually clean up here
		th.DeleteInstance(th.GetStatefulSet(novaNames.MetadataStatefulSetName))

		// then enable it again
		Eventually(func(g Gomega) {
			nova := GetNova(novaNames.NovaName)
			nova.Spec.MetadataServiceTemplate.Enabled = ptr.To(true)

			g.Expect(k8sClient.Update(ctx, nova)).To(Succeed())
		}, timeout, interval).Should(Succeed())

		th.ExpectCondition(
			novaNames.NovaName,
			ConditionGetterFunc(NovaConditionGetter),
			condition.ReadyCondition,
			corev1.ConditionFalse,
		)

		th.SimulateStatefulSetReplicaReady(novaNames.MetadataStatefulSetName)

		th.ExpectCondition(
			novaNames.NovaName,
			ConditionGetterFunc(NovaConditionGetter),
			condition.ReadyCondition,
			corev1.ConditionTrue,
		)
		nova = GetNova(novaNames.NovaName)
		Expect(nova.Status.MetadataServiceReadyCount).To(Equal(int32(1)))
	})

	It("reconfigures nova-metadata service if metadata shared secret is changed", func() {
		originalHash := GetEnvVarValue(
			th.GetStatefulSet(novaNames.MetadataStatefulSetName).Spec.Template.Spec.Containers[0].Env, "CONFIG_HASH", "")
		Expect(originalHash).NotTo(BeEmpty())

		originalComputeHash := GetNovaMetadata(
			novaNames.MetadataName).Status.Hash[novaNames.MetadataNeutronConfigDataName.Name]
		Expect(originalComputeHash).NotTo(BeEmpty())

		secretName := types.NamespacedName{Namespace: novaNames.NovaName.Namespace, Name: SecretName}
		th.UpdateSecret(secretName, "MetadataSecret", []byte("new-metadata-secret"))

		Eventually(func(g Gomega) {
			configDataMap := th.GetSecret(novaNames.MetadataConfigDataName)

			g.Expect(configDataMap.Data).Should(HaveKey("01-nova.conf"))
			conf := string(configDataMap.Data["01-nova.conf"])
			g.Expect(conf).Should(ContainSubstring(("metadata_proxy_shared_secret = new-metadata-secret")))
		}, timeout, interval).Should(Succeed())

		// Assert that the config hash is updated in each stateful set
		Eventually(func(g Gomega) {
			newHash := GetEnvVarValue(
				th.GetStatefulSet(novaNames.MetadataStatefulSetName).Spec.Template.Spec.Containers[0].Env, "CONFIG_HASH", "")
			g.Expect(newHash).NotTo(BeEmpty())
			g.Expect(newHash).NotTo(Equal(originalHash))
		}, timeout, interval).Should(Succeed())

		//Assert that the compute config is updated too
		computeConfigData := th.GetSecret(novaNames.MetadataNeutronConfigDataName)
		Expect(computeConfigData).ShouldNot(BeNil())
		Expect(computeConfigData.Data).Should(HaveKey("05-nova-metadata.conf"))
		configData := string(computeConfigData.Data["05-nova-metadata.conf"])
		Expect(configData).To(ContainSubstring("metadata_proxy_shared_secret = new-metadata-secret"))

		newComputeHash := GetNovaMetadata(
			novaNames.MetadataName).Status.Hash[novaNames.MetadataNeutronConfigDataName.Name]
		Expect(originalComputeHash).NotTo(Equal(newComputeHash))
	})
})
