import Foundation

public enum AttributionJSONValue: Codable, Equatable, Sendable {
    case string(String)
    case number(Double)
    case bool(Bool)
    case array([AttributionJSONValue])
    case object([String: AttributionJSONValue])
    case null

    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self = .null
        } else if let value = try? container.decode(Bool.self) {
            self = .bool(value)
        } else if let value = try? container.decode(Double.self) {
            self = .number(value)
        } else if let value = try? container.decode(String.self) {
            self = .string(value)
        } else if let value = try? container.decode([AttributionJSONValue].self) {
            self = .array(value)
        } else {
            self = .object(try container.decode([String: AttributionJSONValue].self))
        }
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case let .string(value): try container.encode(value)
        case let .number(value): try container.encode(value)
        case let .bool(value): try container.encode(value)
        case let .array(value): try container.encode(value)
        case let .object(value): try container.encode(value)
        case .null: try container.encodeNil()
        }
    }
}

public enum AttributionConsentState: String, Codable, Sendable {
    case granted
    case denied
    case notRequired = "not_required"
    case unknown
}

public enum AttributionConversionAuthority: String, Codable, Sendable {
    case managedApple = "managed_apple"
    case externalMMP = "external_mmp"
    case externalProvider = "external_provider"
    case none
}

public enum AttributionAppleDriver: String, Codable, Hashable, Sendable {
    case adAttributionKit = "aak"
    case skAdNetwork = "skan"
}

public enum AttributionCoarseValue: String, Codable, Sendable {
    case low
    case medium
    case high
}

public struct AttributionPropertyDefinition: Codable, Equatable, Sendable {
    public enum ValueType: String, Codable, Sendable {
        case string
        case number
        case boolean
    }

    public let type: ValueType
    public let required: Bool

    public init(type: ValueType, required: Bool = false) {
        self.type = type
        self.required = required
    }
}

public struct AttributionEventDefinition: Codable, Equatable, Sendable {
    public let name: String
    public let properties: [String: AttributionPropertyDefinition]
    public let fineConversionValue: Int?
    public let coarseConversionValue: AttributionCoarseValue?
    public let lockPostback: Bool

    public init(
        name: String,
        properties: [String: AttributionPropertyDefinition] = [:],
        fineConversionValue: Int? = nil,
        coarseConversionValue: AttributionCoarseValue? = nil,
        lockPostback: Bool = false
    ) {
        self.name = name
        self.properties = properties
        self.fineConversionValue = fineConversionValue
        self.coarseConversionValue = coarseConversionValue
        self.lockPostback = lockPostback
    }
}

public struct AttributionReleaseManifest: Codable, Equatable, Sendable {
    public let manifestVersion: String
    public let appId: String
    public let bundleId: String
    public let environment: String
    public let collectorOrigin: URL
    public let conversionAuthority: AttributionConversionAuthority
    public let conversionSchemaVersion: String
    public let eventSchemaVersion: String
    public let enabledAppleDrivers: Set<AttributionAppleDriver>
    public let associatedDomains: [String]
    public let configurationDigest: String
    public let events: [AttributionEventDefinition]
    public let appAttestEnabled: Bool

    public init(
        manifestVersion: String,
        appId: String,
        bundleId: String,
        environment: String,
        collectorOrigin: URL,
        conversionAuthority: AttributionConversionAuthority,
        conversionSchemaVersion: String,
        eventSchemaVersion: String,
        enabledAppleDrivers: Set<AttributionAppleDriver>,
        associatedDomains: [String],
        configurationDigest: String,
        events: [AttributionEventDefinition],
        appAttestEnabled: Bool = false
    ) throws {
        guard UUID(uuidString: appId) != nil else {
            throw AttributionRuntimeError.invalidManifest("appId")
        }
        guard !manifestVersion.isEmpty else {
            throw AttributionRuntimeError.invalidManifest("manifestVersion")
        }
        guard !bundleId.isEmpty else {
            throw AttributionRuntimeError.invalidManifest("bundleId")
        }
        guard collectorOrigin.scheme == "https", collectorOrigin.host != nil,
              collectorOrigin.user == nil, collectorOrigin.password == nil,
              collectorOrigin.query == nil, collectorOrigin.fragment == nil else {
            throw AttributionRuntimeError.invalidManifest("collectorOrigin")
        }
        let digest = configurationDigest.dropFirst("sha256:".count)
        guard configurationDigest.hasPrefix("sha256:"), configurationDigest.count == 71,
              digest.allSatisfy({ $0.isHexDigit && !$0.isUppercase }) else {
            throw AttributionRuntimeError.invalidManifest("configurationDigest")
        }
        guard !environment.isEmpty, !eventSchemaVersion.isEmpty, !conversionSchemaVersion.isEmpty else {
            throw AttributionRuntimeError.invalidManifest("versioning")
        }
        guard associatedDomains.allSatisfy({ !$0.isEmpty && !$0.contains("/") && !$0.contains(":") }) else {
            throw AttributionRuntimeError.invalidManifest("associatedDomains")
        }
        var names = Set<String>()
        for event in events {
            guard Self.validEventName(event.name), names.insert(event.name).inserted else {
                throw AttributionRuntimeError.invalidManifest("events")
            }
            if let value = event.fineConversionValue, !(0...63).contains(value) {
                throw AttributionRuntimeError.invalidManifest("fineConversionValue")
            }
        }
        self.manifestVersion = manifestVersion
        self.appId = appId.lowercased()
        self.bundleId = bundleId
        self.environment = environment
        self.collectorOrigin = collectorOrigin
        self.conversionAuthority = conversionAuthority
        self.conversionSchemaVersion = conversionSchemaVersion
        self.eventSchemaVersion = eventSchemaVersion
        self.enabledAppleDrivers = enabledAppleDrivers
        self.associatedDomains = associatedDomains
        self.configurationDigest = configurationDigest
        self.events = events
        self.appAttestEnabled = appAttestEnabled
    }

    public static func decode(_ data: Data) throws -> AttributionReleaseManifest {
        let decoded = try JSONDecoder().decode(AttributionReleaseManifest.self, from: data)
        return try AttributionReleaseManifest(
            manifestVersion: decoded.manifestVersion,
            appId: decoded.appId,
            bundleId: decoded.bundleId,
            environment: decoded.environment,
            collectorOrigin: decoded.collectorOrigin,
            conversionAuthority: decoded.conversionAuthority,
            conversionSchemaVersion: decoded.conversionSchemaVersion,
            eventSchemaVersion: decoded.eventSchemaVersion,
            enabledAppleDrivers: decoded.enabledAppleDrivers,
            associatedDomains: decoded.associatedDomains,
            configurationDigest: decoded.configurationDigest,
            events: decoded.events,
            appAttestEnabled: decoded.appAttestEnabled
        )
    }

    public func event(named name: String) -> AttributionEventDefinition? {
        events.first { $0.name == name }
    }

    private static func validEventName(_ value: String) -> Bool {
        guard let first = value.utf8.first, (97...122).contains(first), value.utf8.count <= 100 else {
            return false
        }
        return value.utf8.allSatisfy { (97...122).contains($0) || (48...57).contains($0) || $0 == 95 }
    }
}

public struct AttributionDiagnostics: Codable, Equatable, Sendable {
    public let runtimeVersion: String
    public let manifestVersion: String?
    public let appInstanceId: String?
    public let queuedRecordCount: Int
    public let oldestQueuedAt: Date?
    public let lastFlushAt: Date?
    public let lastFlushErrorCode: String?
    public let lastDeepLink: URL?
    public let adServicesCollection: String
    public let conversionAuthority: AttributionConversionAuthority?
}

public struct AttributionFlushResult: Codable, Equatable, Sendable {
    public let attempted: Int
    public let acknowledged: Int
    public let remaining: Int
}

public enum AttributionRuntimeError: Error, Equatable, LocalizedError, Sendable {
    case invalidManifest(String)
    case manifestNotFound
    case bundleIdentifierMismatch
    case notReady
    case undeclaredEvent(String)
    case undeclaredProperty(event: String, property: String)
    case missingProperty(event: String, property: String)
    case invalidPropertyType(event: String, property: String)
    case queueUnavailable
    case transport(Int)

    public var errorDescription: String? {
        switch self {
        case let .invalidManifest(field): return "AttributionKit release manifest has an invalid \(field)."
        case .manifestNotFound: return "AttributionKit release manifest is missing."
        case .bundleIdentifierMismatch: return "AttributionKit release manifest does not match this app bundle."
        case .notReady: return "AttributionKit runtime is not ready."
        case let .undeclaredEvent(event): return "AttributionKit event \(event) is not declared."
        case let .undeclaredProperty(event, property): return "AttributionKit property \(property) is not declared for \(event)."
        case let .missingProperty(event, property): return "AttributionKit property \(property) is required for \(event)."
        case let .invalidPropertyType(event, property): return "AttributionKit property \(property) has the wrong type for \(event)."
        case .queueUnavailable: return "AttributionKit local queue is unavailable."
        case let .transport(status): return "AttributionKit collector returned HTTP \(status)."
        }
    }
}
