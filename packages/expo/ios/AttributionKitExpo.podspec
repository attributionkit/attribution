Pod::Spec.new do |spec|
  spec.name = 'AttributionKitExpo'
  spec.version = '0.1.0-preview.5'
  spec.summary = 'Native attribution runtime and thin Expo Modules bridge.'
  spec.description = 'Expo lifecycle/bootstrap and typed JavaScript bridge over the shared AttributionCore Swift implementation.'
  spec.homepage = 'https://github.com/attributionkit/attribution'
  spec.license = { type: 'Apache-2.0', file: '../LICENSE' }
  spec.author = { 'AttributionKit' => 'opensource@attribution.sh' }
  spec.source = { git: 'https://github.com/attributionkit/attribution.git', tag: "v#{spec.version}" }
  spec.ios.deployment_target = '15.1'
  spec.swift_version = '5.9'
  spec.static_framework = true
  spec.dependency 'ExpoModulesCore'
  spec.frameworks = ['AdServices', 'AdAttributionKit', 'DeviceCheck', 'StoreKit']
  spec.libraries = ['sqlite3']
  spec.source_files = '**/*.swift'
  spec.resource_bundles = {
    'AttributionKitExpoPrivacy' => ['PrivacyInfo.xcprivacy']
  }
  spec.pod_target_xcconfig = { 'DEFINES_MODULE' => 'YES' }
end
