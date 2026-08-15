Pod::Spec.new do |spec|
  spec.name = 'AttributionCore'
  spec.version = '0.1.0-preview.1'
  spec.summary = 'Auditable AdAttributionKit and SKAdNetwork conversion runtime.'
  spec.description = 'A small Swift runtime that maps typed conversion events to Apple conversion values without identifiers, networking, or user-level data.'
  spec.homepage = 'https://github.com/attributionkit/attribution'
  spec.license = {
    type: 'Apache-2.0',
    text: File.read(File.expand_path('../../LICENSE', __dir__))
  }
  spec.author = { 'AttributionKit' => 'opensource@attribution.sh' }
  spec.source = { git: 'https://github.com/attributionkit/attribution.git', tag: "v#{spec.version}" }
  spec.ios.deployment_target = '15.0'
  spec.swift_version = '5.9'
  spec.source_files = ['Sources/AttributionCore/**/*.swift', 'native/AttributionCore/Sources/AttributionCore/**/*.swift']
  spec.resource_bundles = {
    'AttributionCorePrivacy' => ['Sources/AttributionCore/PrivacyInfo.xcprivacy', 'native/AttributionCore/Sources/AttributionCore/PrivacyInfo.xcprivacy']
  }
end
