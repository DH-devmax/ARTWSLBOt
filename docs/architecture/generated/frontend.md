# Frontend architecture

Generated from code evidence; not production acceptance.

```mermaid
%% Generated from source evidence. Do not hand edit.
%% Import / declaration edges are not proof of runtime activation.
flowchart LR
  n8ac9d1a8a68b["web/src/components/core/charts/art-bar-chart/index.vue"]
  nf18a683b8346["web/src/components/core/charts/art-ring-chart/index.vue"]
  n58805581f9df["web/src/components/core/charts/useChartSurface.ts"]
  nc56b3e443eca["web/src/types/component/chart.ts"]
  nef9b3daf6480["AIGateway.vue"]
  nf3c1b0779702["AccountDashboard.vue"]
  n60e142e002e5["AccountLogin.vue"]
  n454cf47c4045["AccountRemoval.vue"]
  n2745a379c1e3["AdminMark.vue"]
  ne8f684acb76f["App.vue"]
  n6b6ed397b0fb["BillingCenter.vue"]
  nd3925f36e9ea["BusinessModuleDialog.vue"]
  n59bf91d15aad["BusinessRuleEditor.vue"]
  n7dbabb926db9["ChiliMessage.vue"]
  n9b831b1c2eb1["ControlledOnboarding.vue"]
  nd72ee123d782["CustomerBindingReview.vue"]
  n52fba6f8fc31["CustomerInvitation.vue"]
  n9635abf4cfa4["CustomerInvitationDialog.vue"]
  n6b09e2320d96["CustomerOnboarding.vue"]
  n49f0072e169c["DecodePerformance.vue"]
  n0ede1affe420["FeatureCenter.vue"]
  n6e8a4795416a["FeatureRulePreview.vue"]
  n674d15293561["GroupAssignment.vue"]
  nd7b6e702fab3["GroupBusinessRuntime.vue"]
  n0f054370a2e3["LajiaoLogin.vue"]
  n99753060b311["MaiBotConsole.vue"]
  nc634df51a6b0["MediaComposer.vue"]
  naf8c4d7a5543["MessageCenter.vue"]
  ne46f4a054715["MessageComposer.vue"]
  n0bf20317fa6a["MessageContact.vue"]
  na50bf49b2b1f["MessageForward.vue"]
  na62cf88e5e14["MessageImage.vue"]
  n9c7f5cbd423a["MessageVideo.vue"]
  n119a8cf24e8a["MobileSearchAccess.vue"]
  na4b97b43b041["NativeCapabilities.vue"]
  nb236e5936e3a["NativeDeployment.vue"]
  n7c2b91204098["NativeLogin.vue"]
  nfc7946cd170a["NativeMembers.vue"]
  n28b26eb6da43["NativeRecalls.vue"]
  n35762220b9d2["NativeSecurity.vue"]
  nee8a9654ed64["NativeSettings.vue"]
  nf9d9baa418d3["NativeSocial.vue"]
  nd87a29eb8584["OnboardingFriendGate.vue"]
  n3fb51656ce84["OperatorDemoClaim.vue"]
  n83b46f974894["RobotWorkbench.vue"]
  n3200f1294f81["SystemMonitor.vue"]
  nee0d0c236536["TenantCapabilities.vue"]
  n642ddefaa16b["TenantMembers.vue"]
  nd9c55f472926["TestTools.vue"]
  nf9b64a0e847b["UserCenter.vue"]
  n3bb8d28fc696["UserGroupAction.vue"]
  nb790c63fcaaf["WebCaptcha.vue"]
  n8a90b36f7805["WorkspaceInsights.vue"]
  ne3b6123a5ff6["WorkspaceSwitcher.vue"]
  n202294e5adec["accountIdentity.ts"]
  na7f7e0b4e419["accountLive.ts"]
  n37662f3b7d3a["api.ts"]
  n71a25ab8d034["assistant/AssistantDrawer.vue"]
  ne98ef3d9af76["assistant/AssistantGovernance.vue"]
  nb55be40423c5["assistant/AssistantHelp.vue"]
  nba87acca6bd7["assistant/AssistantInput.vue"]
  n730d81a25219["assistant/AssistantMarkdown.vue"]
  n769065bf5b4c["assistant/AssistantShell.vue"]
  n1b43d4dc0f9e["assistant/ConfigDiff.vue"]
  n4dd3f9ef6aff["assistant/ConfigValue.vue"]
  ne8820e3f59c7["assistant/ConversationHistory.vue"]
  n34037e977e5f["assistant/GroupPicker.vue"]
  neb75ca0245f7["assistant/ModuleExposure.vue"]
  n56d39e26c438["assistant/PlatformAssistant.vue"]
  n462346f8fc0e["assistant/ResultCard.vue"]
  nc68f7a415c14["assistant/client.ts"]
  nc9fc60b54545["assistant/types.ts"]
  nc6a4ba0c83b8["chiliSecurityCaptcha.ts"]
  nf68bffe1cc14["customer.ts"]
  n81572a64708d["featureCatalog.ts"]
  nced7725a6bd7["main.ts"]
  n248679f6fbb9["messageMedia.ts"]
  n6093d03dde3b["messageQuote.ts"]
  nf2d6ebab89f2["messageSending.ts"]
  n5522c566747f["monitorLive.ts"]
  nf1fb8c45ccf8["nativeCaptcha.ts"]
  n7690099edd29["nativeSettings.ts"]
  n0f124c20c400["store.ts"]
  n0c1cb75639a9["style.css"]
  ne72adf63747f["theme/lumenwave/system.css"]
  na440e3631d4e["theme/product.css"]
  nbe38b8714585["workspaces.ts"]
  n8ac9d1a8a68b -->|"import"| n58805581f9df
  n8ac9d1a8a68b -->|"type-import"| nc56b3e443eca
  nf18a683b8346 -->|"import"| n58805581f9df
  nf18a683b8346 -->|"type-import"| nc56b3e443eca
  nef9b3daf6480 -->|"import"| n37662f3b7d3a
  nf3c1b0779702 -->|"import"| n8ac9d1a8a68b
  nf3c1b0779702 -->|"import"| n119a8cf24e8a
  nf3c1b0779702 -->|"import"| nee8a9654ed64
  nf3c1b0779702 -->|"import"| nd87a29eb8584
  nf3c1b0779702 -->|"import"| n202294e5adec
  nf3c1b0779702 -->|"import"| na7f7e0b4e419
  nf3c1b0779702 -->|"import"| n37662f3b7d3a
  n60e142e002e5 -->|"import"| n0f054370a2e3
  n60e142e002e5 -->|"import"| n7c2b91204098
  n60e142e002e5 -->|"type-import"| n37662f3b7d3a
  n60e142e002e5 -->|"import"| nbe38b8714585
  n454cf47c4045 -->|"import"| n37662f3b7d3a
  ne8f684acb76f -->|"import"| nef9b3daf6480
  ne8f684acb76f -->|"import"| nf3c1b0779702
  ne8f684acb76f -->|"import"| n60e142e002e5
  ne8f684acb76f -->|"import"| n454cf47c4045
  ne8f684acb76f -->|"import"| n2745a379c1e3
  ne8f684acb76f -->|"import"| n6b6ed397b0fb
  ne8f684acb76f -->|"import"| nd3925f36e9ea
  ne8f684acb76f -->|"import"| nd72ee123d782
  ne8f684acb76f -->|"import"| n52fba6f8fc31
  ne8f684acb76f -->|"import"| n9635abf4cfa4
  ne8f684acb76f -->|"import"| n0ede1affe420
  ne8f684acb76f -->|"import"| n674d15293561
  ne8f684acb76f -->|"import"| nd7b6e702fab3
  ne8f684acb76f -->|"import"| n99753060b311
  ne8f684acb76f -->|"import"| naf8c4d7a5543
  ne8f684acb76f -->|"import"| nb236e5936e3a
  ne8f684acb76f -->|"import"| n83b46f974894
  ne8f684acb76f -->|"import"| n3200f1294f81
  ne8f684acb76f -->|"import"| nee0d0c236536
  ne8f684acb76f -->|"import"| n642ddefaa16b
  ne8f684acb76f -->|"import"| nd9c55f472926
  ne8f684acb76f -->|"import"| nf9b64a0e847b
  ne8f684acb76f -->|"import"| nb790c63fcaaf
  ne8f684acb76f -->|"import"| n8a90b36f7805
  ne8f684acb76f -->|"import"| ne3b6123a5ff6
  ne8f684acb76f -->|"import"| n202294e5adec
  ne8f684acb76f -->|"import"| n37662f3b7d3a
  ne8f684acb76f -->|"import"| n71a25ab8d034
  ne8f684acb76f -->|"import"| ne98ef3d9af76
  ne8f684acb76f -->|"import"| nf68bffe1cc14
  ne8f684acb76f -->|"import"| n0f124c20c400
  ne8f684acb76f -->|"import"| nbe38b8714585
  n6b6ed397b0fb -->|"import"| n37662f3b7d3a
  n6b6ed397b0fb -->|"import"| nf68bffe1cc14
  n6b6ed397b0fb -->|"import"| n0f124c20c400
  nd3925f36e9ea -->|"type-import"| n37662f3b7d3a
  n7dbabb926db9 -->|"import"| na62cf88e5e14
  n7dbabb926db9 -->|"import"| n9c7f5cbd423a
  n7dbabb926db9 -->|"type-import"| n37662f3b7d3a
  n7dbabb926db9 -->|"type-import"| nbe38b8714585
  n9b831b1c2eb1 -->|"import"| n37662f3b7d3a
  n9b831b1c2eb1 -->|"import"| nf68bffe1cc14
  n9b831b1c2eb1 -->|"import"| n0f124c20c400
  nd72ee123d782 -->|"import"| n37662f3b7d3a
  nd72ee123d782 -->|"import"| nf68bffe1cc14
  nd72ee123d782 -->|"import"| n0f124c20c400
  n52fba6f8fc31 -->|"import"| n37662f3b7d3a
  n52fba6f8fc31 -->|"import"| nf68bffe1cc14
  n9635abf4cfa4 -->|"import"| n37662f3b7d3a
  n9635abf4cfa4 -->|"import"| nf68bffe1cc14
  n9635abf4cfa4 -->|"import"| n0f124c20c400
  n6b09e2320d96 -->|"import"| nb790c63fcaaf
  n6b09e2320d96 -->|"import"| n37662f3b7d3a
  n6b09e2320d96 -->|"import"| nf68bffe1cc14
  n6b09e2320d96 -->|"import"| n0f124c20c400
  n49f0072e169c -->|"import"| n8ac9d1a8a68b
  n49f0072e169c -->|"type-import"| n37662f3b7d3a
  n0ede1affe420 -->|"import"| n59bf91d15aad
  n0ede1affe420 -->|"import"| n6e8a4795416a
  n0ede1affe420 -->|"import"| na4b97b43b041
  n0ede1affe420 -->|"import"| n37662f3b7d3a
  n0ede1affe420 -->|"type-import"| n81572a64708d
  n0ede1affe420 -->|"import"| n0f124c20c400
  n6e8a4795416a -->|"type-import"| n37662f3b7d3a
  n674d15293561 -->|"import"| n37662f3b7d3a
  n674d15293561 -->|"import"| n0f124c20c400
  nd7b6e702fab3 -->|"import"| n37662f3b7d3a
  nd7b6e702fab3 -->|"import"| nf68bffe1cc14
  nd7b6e702fab3 -->|"import"| n0f124c20c400
  n0f054370a2e3 -->|"import"| n37662f3b7d3a
  n0f054370a2e3 -->|"type-import"| nbe38b8714585
  n99753060b311 -->|"import"| n37662f3b7d3a
  n99753060b311 -->|"import"| n0f124c20c400
  nc634df51a6b0 -->|"import"| n37662f3b7d3a
  nc634df51a6b0 -->|"import"| nf2d6ebab89f2
  nc634df51a6b0 -->|"type-import"| nf2d6ebab89f2
  nc634df51a6b0 -->|"type-import"| nbe38b8714585
  naf8c4d7a5543 -->|"import"| n7dbabb926db9
  naf8c4d7a5543 -->|"import"| ne46f4a054715
  naf8c4d7a5543 -->|"import"| n0bf20317fa6a
  naf8c4d7a5543 -->|"import"| na50bf49b2b1f
  naf8c4d7a5543 -->|"import"| na62cf88e5e14
  naf8c4d7a5543 -->|"import"| n9c7f5cbd423a
  naf8c4d7a5543 -->|"import"| nfc7946cd170a
  naf8c4d7a5543 -->|"import"| n28b26eb6da43
  naf8c4d7a5543 -->|"import"| na7f7e0b4e419
  naf8c4d7a5543 -->|"import"| n37662f3b7d3a
  naf8c4d7a5543 -->|"import"| n6093d03dde3b
  naf8c4d7a5543 -->|"type-import"| nf2d6ebab89f2
  naf8c4d7a5543 -->|"import"| n0f124c20c400
  ne46f4a054715 -->|"import"| nc634df51a6b0
  ne46f4a054715 -->|"import"| n37662f3b7d3a
  ne46f4a054715 -->|"import"| nf2d6ebab89f2
  ne46f4a054715 -->|"type-import"| nf2d6ebab89f2
  ne46f4a054715 -->|"import"| n0f124c20c400
  ne46f4a054715 -->|"type-import"| nbe38b8714585
  n0bf20317fa6a -->|"import"| na62cf88e5e14
  n0bf20317fa6a -->|"type-import"| nbe38b8714585
  na50bf49b2b1f -->|"import"| na62cf88e5e14
  na50bf49b2b1f -->|"import"| n9c7f5cbd423a
  na50bf49b2b1f -->|"type-import"| n37662f3b7d3a
  na50bf49b2b1f -->|"type-import"| nbe38b8714585
  na62cf88e5e14 -->|"import"| n248679f6fbb9
  na62cf88e5e14 -->|"type-import"| nbe38b8714585
  n9c7f5cbd423a -->|"import"| n248679f6fbb9
  n9c7f5cbd423a -->|"type-import"| nbe38b8714585
  n119a8cf24e8a -->|"import"| n7c2b91204098
  n119a8cf24e8a -->|"import"| n202294e5adec
  n119a8cf24e8a -->|"import"| n37662f3b7d3a
  n119a8cf24e8a -->|"import"| n0f124c20c400
  na4b97b43b041 -->|"import"| nf18a683b8346
  na4b97b43b041 -->|"import"| nf9d9baa418d3
  na4b97b43b041 -->|"import"| n37662f3b7d3a
  na4b97b43b041 -->|"import"| n81572a64708d
  na4b97b43b041 -->|"import"| n0f124c20c400
  nb236e5936e3a -->|"import"| n37662f3b7d3a
  n7c2b91204098 -->|"import"| n37662f3b7d3a
  n7c2b91204098 -->|"import"| nf1fb8c45ccf8
  nfc7946cd170a -->|"import"| nee8a9654ed64
  nfc7946cd170a -->|"import"| n37662f3b7d3a
  n28b26eb6da43 -->|"import"| n37662f3b7d3a
  n35762220b9d2 -->|"import"| na62cf88e5e14
  n35762220b9d2 -->|"import"| n37662f3b7d3a
  nee8a9654ed64 -->|"import"| na62cf88e5e14
  nee8a9654ed64 -->|"import"| n35762220b9d2
  nee8a9654ed64 -->|"import"| n202294e5adec
  nee8a9654ed64 -->|"import"| n37662f3b7d3a
  nee8a9654ed64 -->|"import"| n7690099edd29
  nf9d9baa418d3 -->|"import"| n37662f3b7d3a
  nf9d9baa418d3 -->|"import"| n0f124c20c400
  nd87a29eb8584 -->|"import"| n202294e5adec
  nd87a29eb8584 -->|"import"| n37662f3b7d3a
  nd87a29eb8584 -->|"import"| n0f124c20c400
  nd87a29eb8584 -->|"import"| nbe38b8714585
  n3fb51656ce84 -->|"import"| nb790c63fcaaf
  n3fb51656ce84 -->|"import"| n37662f3b7d3a
  n3fb51656ce84 -->|"import"| nf68bffe1cc14
  n3fb51656ce84 -->|"import"| n0f124c20c400
  n83b46f974894 -->|"import"| n8a90b36f7805
  n83b46f974894 -->|"import"| n202294e5adec
  n83b46f974894 -->|"type-import"| n37662f3b7d3a
  n83b46f974894 -->|"import"| nbe38b8714585
  n3200f1294f81 -->|"import"| n37662f3b7d3a
  n3200f1294f81 -->|"import"| n5522c566747f
  n3200f1294f81 -->|"import"| n0f124c20c400
  nee0d0c236536 -->|"import"| n37662f3b7d3a
  nee0d0c236536 -->|"import"| nf68bffe1cc14
  nee0d0c236536 -->|"import"| n0f124c20c400
  n642ddefaa16b -->|"import"| n37662f3b7d3a
  n642ddefaa16b -->|"import"| nf68bffe1cc14
  n642ddefaa16b -->|"import"| n0f124c20c400
  nd9c55f472926 -->|"import"| n9b831b1c2eb1
  nd9c55f472926 -->|"import"| nd87a29eb8584
  nd9c55f472926 -->|"import"| n37662f3b7d3a
  nd9c55f472926 -->|"type-import"| n37662f3b7d3a
  nd9c55f472926 -->|"import"| nf68bffe1cc14
  nd9c55f472926 -->|"import"| n0f124c20c400
  nd9c55f472926 -->|"type-import"| nbe38b8714585
  nf9b64a0e847b -->|"import"| n2745a379c1e3
  nf9b64a0e847b -->|"import"| n6b09e2320d96
  nf9b64a0e847b -->|"import"| nd7b6e702fab3
  nf9b64a0e847b -->|"import"| n3fb51656ce84
  nf9b64a0e847b -->|"import"| n3bb8d28fc696
  nf9b64a0e847b -->|"import"| nb790c63fcaaf
  nf9b64a0e847b -->|"import"| n37662f3b7d3a
  nf9b64a0e847b -->|"import"| nf68bffe1cc14
  nf9b64a0e847b -->|"import"| n0f124c20c400
  n3bb8d28fc696 -->|"import"| n37662f3b7d3a
  n3bb8d28fc696 -->|"import"| nf68bffe1cc14
  n3bb8d28fc696 -->|"import"| n0f124c20c400
  nb790c63fcaaf -->|"import"| n37662f3b7d3a
  nb790c63fcaaf -->|"import"| n0f124c20c400
  n8a90b36f7805 -->|"import"| n8ac9d1a8a68b
  n8a90b36f7805 -->|"import"| nf18a683b8346
  n8a90b36f7805 -->|"import"| n49f0072e169c
  n8a90b36f7805 -->|"type-import"| n37662f3b7d3a
  ne3b6123a5ff6 -->|"import"| nbe38b8714585
  n202294e5adec -->|"type-import"| n37662f3b7d3a
  na7f7e0b4e419 -->|"import"| n37662f3b7d3a
  n37662f3b7d3a -->|"re-export"| nbe38b8714585
  n37662f3b7d3a -->|"type-import"| nbe38b8714585
  n71a25ab8d034 -->|"import"| n2745a379c1e3
  n71a25ab8d034 -->|"import"| n202294e5adec
  n71a25ab8d034 -->|"import"| n37662f3b7d3a
  n71a25ab8d034 -->|"import"| nb55be40423c5
  n71a25ab8d034 -->|"import"| nba87acca6bd7
  n71a25ab8d034 -->|"import"| n730d81a25219
  n71a25ab8d034 -->|"import"| n769065bf5b4c
  n71a25ab8d034 -->|"import"| n1b43d4dc0f9e
  n71a25ab8d034 -->|"import"| ne8820e3f59c7
  n71a25ab8d034 -->|"import"| n34037e977e5f
  n71a25ab8d034 -->|"import"| n462346f8fc0e
  n71a25ab8d034 -->|"import"| nc68f7a415c14
  n71a25ab8d034 -->|"import"| nc9fc60b54545
  n71a25ab8d034 -->|"import"| n0f124c20c400
  ne98ef3d9af76 -->|"import"| n37662f3b7d3a
  ne98ef3d9af76 -->|"import"| neb75ca0245f7
  ne98ef3d9af76 -->|"import"| n0f124c20c400
  nb55be40423c5 -->|"import"| n37662f3b7d3a
  nb55be40423c5 -->|"import"| nc68f7a415c14
  nb55be40423c5 -->|"import"| nc9fc60b54545
  n1b43d4dc0f9e -->|"import"| n4dd3f9ef6aff
  n4dd3f9ef6aff -->|"import"| nc68f7a415c14
  ne8820e3f59c7 -->|"type-import"| n37662f3b7d3a
  n34037e977e5f -->|"import"| n202294e5adec
  n34037e977e5f -->|"type-import"| n37662f3b7d3a
  neb75ca0245f7 -->|"import"| n37662f3b7d3a
  neb75ca0245f7 -->|"import"| n0f124c20c400
  n56d39e26c438 -->|"import"| n37662f3b7d3a
  n56d39e26c438 -->|"import"| n0f124c20c400
  n462346f8fc0e -->|"type-import"| n37662f3b7d3a
  n462346f8fc0e -->|"import"| n4dd3f9ef6aff
  n462346f8fc0e -->|"import"| n34037e977e5f
  n462346f8fc0e -->|"import"| nc68f7a415c14
  nc68f7a415c14 -->|"import"| n37662f3b7d3a
  nc6a4ba0c83b8 -->|"import"| n37662f3b7d3a
  nf68bffe1cc14 -->|"import"| n37662f3b7d3a
  nced7725a6bd7 -->|"import"| ne8f684acb76f
  nced7725a6bd7 -->|"import"| n0c1cb75639a9
  nced7725a6bd7 -->|"import"| ne72adf63747f
  nced7725a6bd7 -->|"import"| na440e3631d4e
  n248679f6fbb9 -->|"import"| n37662f3b7d3a
  n248679f6fbb9 -->|"type-import"| nbe38b8714585
  n5522c566747f -->|"import"| n37662f3b7d3a
  n7690099edd29 -->|"type-import"| n37662f3b7d3a
  n0f124c20c400 -->|"import"| n37662f3b7d3a
  n0f124c20c400 -->|"import"| nbe38b8714585
```
