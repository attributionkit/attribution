import ExpoModulesCore
import Foundation

public final class AttributionKitExpoModule: Module {
    public func definition() -> ModuleDefinition {
        Name("AttributionKit")

        Constant("runtimeVersion") {
            AttributionCore.version
        }

        AsyncFunction("ready") { () async throws -> Void in
            try await Attribution.ready()
        }

        AsyncFunction("trackRaw") { (event: String, propertiesJSON: String, consentState: String) async throws -> Void in
            guard let data = propertiesJSON.data(using: .utf8) else {
                throw AttributionRuntimeError.invalidPropertyType(event: event, property: "properties")
            }
            let properties = try JSONDecoder().decode([String: AttributionJSONValue].self, from: data)
            let consent = AttributionConsentState(rawValue: consentState) ?? .unknown
            try await Attribution.track(event, properties: properties, consentState: consent)
        }

        AsyncFunction("diagnosticsRaw") { () async throws -> String in
            let diagnostics = try await Attribution.getDiagnostics()
            let encoder = JSONEncoder()
            encoder.dateEncodingStrategy = .iso8601
            let data = try encoder.encode(diagnostics)
            guard let json = String(data: data, encoding: .utf8) else {
                throw AttributionCoreError.invalidBundleConfiguration("diagnostics-result")
            }
            return json
        }

        AsyncFunction("flushRaw") { () async throws -> String in
            let result = try await Attribution.flush()
            let data = try JSONEncoder().encode(result)
            guard let json = String(data: data, encoding: .utf8) else {
                throw AttributionCoreError.invalidBundleConfiguration("flush-result")
            }
            return json
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
