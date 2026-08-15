import ExpoModulesCore
import Foundation

public final class AttributionKitExpoModule: Module {
    public func definition() -> ModuleDefinition {
        Name("AttributionKit")

        Constant("runtimeVersion") {
            AttributionCore.version
        }

        Function("schemaHash") {
            try AttributionConfiguration.fromBundle().schemaHash
        }

        Function("conversionValue") { (event: String) in
            try AttributionCore.conversionValue(
                for: event,
                configuration: AttributionConfiguration.fromBundle()
            )
        }

        AsyncFunction("recordRaw") { (event: String) async throws -> String in
            let report = try await AttributionCore.record(event)
            let data = try JSONEncoder().encode(report)
            guard let json = String(data: data, encoding: .utf8) else {
                throw AttributionCoreError.invalidBundleConfiguration("record-result")
            }
            return json
        }
    }
}
