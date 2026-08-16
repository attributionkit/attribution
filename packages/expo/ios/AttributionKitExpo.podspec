Pod::Spec.new do |spec|
  spec.name = 'AttributionKitExpo'
  spec.version = '0.1.0-preview.2'
  spec.summary = 'Auditable AdAttributionKit and SKAdNetwork runtime for Expo.'
  spec.description = 'Expo Modules API bridge and config plugin for the AttributionKit Swift runtime.'
  spec.homepage = 'https://github.com/attributionkit/attribution'
  spec.license = { type: 'Apache-2.0', file: '../LICENSE' }
  spec.author = { 'AttributionKit' => 'opensource@attribution.sh' }
  spec.source = { git: 'https://github.com/attributionkit/attribution.git', tag: "v#{spec.version}" }
  spec.ios.deployment_target = '15.1'
  spec.swift_version = '5.9'
  spec.static_framework = true
  spec.dependency 'ExpoModulesCore'
  spec.source_files = '**/*.swift'
  spec.resource_bundles = {
    'AttributionKitExpoPrivacy' => ['PrivacyInfo.xcprivacy']
  }
  spec.pod_target_xcconfig = { 'DEFINES_MODULE' => 'YES' }
end
