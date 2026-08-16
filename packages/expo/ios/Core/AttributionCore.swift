import Foundation

#if canImport(AdAttributionKit)
import AdAttributionKit
#endif

#if canImport(StoreKit)
import StoreKit
#endif

public enum AttributionCoreError: Error, Equatable, LocalizedError, Sendable {
    case invalidSchemaHash
    case noEvents
    case invalidEventName
    case duplicateEvent(String)
    case duplicateConversionValue(Int)
    case invalidConversionValue(event: String, value: Int)
    case unknownEvent(String)
    case missingBundleConfiguration(String)
    case invalidBundleConfiguration(String)

    public var errorDescription: String? {
        switch self {
        case .invalidSchemaHash:
            return "AttributionKit schema hash must be a lowercase SHA-256 hex digest."
        case .noEvents:
            return "AttributionKit requires at least one conversion event."
        case .invalidEventName:
            return "AttributionKit event names must not be empty."
        case let .duplicateEvent(event):
            return "AttributionKit event \"\(event)\" is duplicated."
        case let .duplicateConversionValue(value):
            return "AttributionKit conversion value \(value) is assigned to more than one event."
        case let .invalidConversionValue(event, value):
            return "AttributionKit event \"\(event)\" has invalid conversion value \(value); expected 0...63."
        case let .unknownEvent(event):
            return "Unknown AttributionKit event \"\(event)\"."
        case let .missingBundleConfiguration(key):
            return "AttributionKit Info.plist key \"\(key)\" is missing."
        case let .invalidBundleConfiguration(key):
            return "AttributionKit Info.plist key \"\(key)\" is invalid."
        }
    }
}

public struct AttributionConfiguration: Equatable, Sendable {
    public static let schemaHashPlistKey = "AttributionKitSchemaHash"
    public static let eventValuesPlistKey = "AttributionKitEventValues"

    public let schemaHash: String
    public let eventValues: [String: Int]

    public init(schemaHash: String, eventValues: [String: Int]) throws {
        guard schemaHash.utf8.count == 64,
              schemaHash.utf8.allSatisfy({ (48...57).contains($0) || (97...102).contains($0) }) else {
            throw AttributionCoreError.invalidSchemaHash
        }
        guard !eventValues.isEmpty else {
            throw AttributionCoreError.noEvents
        }
        var usedValues = Set<Int>()
        for (event, value) in eventValues {
            guard !event.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
                throw AttributionCoreError.invalidEventName
            }
            guard (0...63).contains(value) else {
                throw AttributionCoreError.invalidConversionValue(event: event, value: value)
            }
            guard usedValues.insert(value).inserted else {
                throw AttributionCoreError.duplicateConversionValue(value)
            }
        }
        self.schemaHash = schemaHash
        self.eventValues = eventValues
    }

    public init(schemaHash: String, events: [String]) throws {
        guard !events.isEmpty else {
            throw AttributionCoreError.noEvents
        }
        var values: [String: Int] = [:]
        for (index, event) in events.enumerated() {
            if values[event] != nil {
                throw AttributionCoreError.duplicateEvent(event)
            }
            guard index <= 63 else {
                throw AttributionCoreError.invalidConversionValue(event: event, value: index)
            }
            values[event] = index
        }
        try self.init(schemaHash: schemaHash, eventValues: values)
    }

    public static func fromBundle(_ bundle: Bundle = .main) throws -> AttributionConfiguration {
        guard let schemaHash = bundle.object(forInfoDictionaryKey: schemaHashPlistKey) as? String else {
            throw AttributionCoreError.missingBundleConfiguration(schemaHashPlistKey)
        }
        guard let rawValues = bundle.object(forInfoDictionaryKey: eventValuesPlistKey) as? [String: Any] else {
            throw AttributionCoreError.missingBundleConfiguration(eventValuesPlistKey)
        }

        let values = try parseEventValues(rawValues)
        return try AttributionConfiguration(schemaHash: schemaHash, eventValues: values)
    }

    static func parseEventValues(_ rawValues: [String: Any]) throws -> [String: Int] {
        var values: [String: Int] = [:]
        for (event, rawValue) in rawValues {
            guard let number = rawValue as? NSNumber,
                  CFGetTypeID(number) != CFBooleanGetTypeID(),
                  !["f", "d"].contains(String(cString: number.objCType)) else {
                throw AttributionCoreError.invalidBundleConfiguration(eventValuesPlistKey)
            }
            values[event] = number.intValue
        }
        return values
    }

    public func conversionValue(for event: String) throws -> Int {
        guard let value = eventValues[event] else {
            throw AttributionCoreError.unknownEvent(event)
        }
        return value
    }
}

public struct AttributionBackendResult: Codable, Equatable, Sendable {
    public enum Status: String, Codable, Sendable {
        case succeeded
        case failed
        case unavailable
    }

    public let status: Status
    public let error: String?

    public static let succeeded = AttributionBackendResult(status: .succeeded, error: nil)
    public static let unavailable = AttributionBackendResult(status: .unavailable, error: nil)

    public static func failed(_ error: Error) -> AttributionBackendResult {
        AttributionBackendResult(status: .failed, error: String(describing: error))
    }
}

public struct AttributionUpdateReport: Codable, Equatable, Sendable {
    public let event: String
    public let fineConversionValue: Int
    public let schemaHash: String
    public let adAttributionKit: AttributionBackendResult
    public let skAdNetwork: AttributionBackendResult
}

public enum AttributionCore {
    public static let version = "0.1.0-preview.3"

    public static func conversionValue(
        for event: String,
        configuration: AttributionConfiguration
    ) throws -> Int {
        try configuration.conversionValue(for: event)
    }

    public static func record(
        _ event: String,
        configuration: AttributionConfiguration
    ) async throws -> AttributionUpdateReport {
        let value = try configuration.conversionValue(for: event)

        // Apple explicitly permits updating AdAttributionKit and SKAdNetwork
        // independently when campaigns may use either framework. Both calls are
        // made by this one semantic owner; transport SDKs must not update values.
        async let aak = updateAdAttributionKit(value)
        async let skan = updateSKAdNetwork(value)

        return await AttributionUpdateReport(
            event: event,
            fineConversionValue: value,
            schemaHash: configuration.schemaHash,
            adAttributionKit: aak,
            skAdNetwork: skan
        )
    }

    public static func record(
        _ event: String,
        bundle: Bundle = .main
    ) async throws -> AttributionUpdateReport {
        try await record(event, configuration: AttributionConfiguration.fromBundle(bundle))
    }

    private static func updateAdAttributionKit(_ value: Int) async -> AttributionBackendResult {
        #if os(iOS) && canImport(AdAttributionKit)
        if #available(iOS 17.4, *) {
            do {
                try await Postback.updateConversionValue(value, lockPostback: false)
                return .succeeded
            } catch {
                return .failed(error)
            }
        }
        #endif
        return .unavailable
    }

    private static func updateSKAdNetwork(_ value: Int) async -> AttributionBackendResult {
        #if os(iOS) && canImport(StoreKit)
        if #available(iOS 15.4, *) {
            return await withCheckedContinuation { continuation in
                SKAdNetwork.updatePostbackConversionValue(value) { error in
                    if let error {
                        continuation.resume(returning: .failed(error))
                    } else {
                        continuation.resume(returning: .succeeded)
                    }
                }
            }
        }
        #endif
        return .unavailable
    }
}
