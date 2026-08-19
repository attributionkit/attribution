import Foundation

#if canImport(AdServices)
import AdServices
#endif

public actor AttributionClient {
    private static let retention: TimeInterval = 30 * 24 * 60 * 60
    private static let maximumQueueSize = 10_000

    public let manifest: AttributionReleaseManifest
    private let outbox: AttributionOutbox
    private let transport: any AttributionTransporting
    private let bundle: Bundle
    private var readyState = false

    public init(
        manifest: AttributionReleaseManifest,
        storageURL: URL,
        bundle: Bundle = .main
    ) throws {
        self.manifest = manifest
        self.bundle = bundle
        self.transport = AttributionHTTPTransport()
        try FileManager.default.createDirectory(
            at: storageURL.deletingLastPathComponent(),
            withIntermediateDirectories: true
        )
        #if os(iOS)
        try? FileManager.default.setAttributes(
            [.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication],
            ofItemAtPath: storageURL.deletingLastPathComponent().path
        )
        #endif
        outbox = try AttributionOutbox(path: storageURL.path)
    }

    init(
        manifest: AttributionReleaseManifest,
        storageURL: URL,
        bundle: Bundle,
        transport: any AttributionTransporting
    ) throws {
        self.manifest = manifest
        self.bundle = bundle
        self.transport = transport
        try FileManager.default.createDirectory(
            at: storageURL.deletingLastPathComponent(),
            withIntermediateDirectories: true
        )
        outbox = try AttributionOutbox(path: storageURL.path)
    }

    public func ready(launchURL: URL? = nil) async throws {
        if readyState {
            if let launchURL { try captureDeepLink(launchURL) }
            return
        }
        if let observed = bundle.bundleIdentifier, observed != manifest.bundleId {
            throw AttributionRuntimeError.bundleIdentifierMismatch
        }
        let now = Date()
        try outbox.prune(now: now, maximumRecords: Self.maximumQueueSize)
        let appInstanceId = try installationIdentifier(now: now)
        if let launchURL { try captureDeepLink(launchURL, now: now) }
        try collectAdServicesTokenIfNeeded(now: now)
        readyState = true
        _ = try? await flush()
        _ = appInstanceId
    }

    public func track(
        _ event: String,
        properties: [String: AttributionJSONValue] = [:],
        consentState: AttributionConsentState = .unknown
    ) async throws {
        guard readyState else { throw AttributionRuntimeError.notReady }
        guard let definition = manifest.event(named: event) else {
            throw AttributionRuntimeError.undeclaredEvent(event)
        }
        try validate(properties: properties, for: definition)
        // Until App Attest establishes an authoritative client assertion, the
        // backend deliberately reduces this value to unknown. Keep it out of
        // provider properties so a client cannot smuggle consent state into a
        // provider mapping.
        _ = consentState
        try enqueue(event: event, properties: properties, kind: .event, occurredAt: Date())
        if manifest.conversionAuthority == .managedApple, let value = definition.fineConversionValue {
            try await updateConversions(
                fine: value,
                coarse: definition.coarseConversionValue,
                lock: definition.lockPostback
            )
        }
        _ = try? await flush()
    }

    public func captureDeepLink(_ url: URL) throws {
        try captureDeepLink(url, now: Date())
    }

    public func flush() async throws -> AttributionFlushResult {
        guard readyState else { throw AttributionRuntimeError.notReady }
        let now = Date()
        try outbox.prune(now: now, maximumRecords: Self.maximumQueueSize)
        let appInstanceId = try requireStateString("app_instance_id")
        let token = try await appInstanceToken(appInstanceId: appInstanceId)
        let records = try outbox.due(limit: 20, now: now)
        var acknowledged = 0
        var lastError: Error?
        for record in records {
            do {
                try await transport.send(
                    record: record,
                    appId: manifest.appId,
                    appInstanceId: appInstanceId,
                    token: token,
                    origin: manifest.collectorOrigin,
                    eventSchemaVersion: manifest.eventSchemaVersion
                )
                try outbox.acknowledge(record.id)
				if record.kind == .adServicesToken {
					try outbox.setState("adservices_status", value: Data("exchanged".utf8))
				}
                acknowledged += 1
            } catch {
                let attempt = record.attemptCount + 1
                let maximum = min(pow(2, Double(min(attempt, 12))), 21_600)
                let delay = Double.random(in: 0...maximum)
                try outbox.reschedule(record.id, attemptCount: attempt, nextAttemptAt: now.addingTimeInterval(delay))
                lastError = error
            }
        }
        try outbox.setState("last_flush_at", value: Data(Self.timestamp(now).utf8))
        if let lastError {
            let code: String
            if case let AttributionRuntimeError.transport(status) = lastError {
                code = "http_\(status)"
            } else {
                code = "network_error"
            }
            try outbox.setState("last_flush_error", value: Data(code.utf8))
        } else {
            try outbox.setState("last_flush_error", value: Data())
        }
        let statistics = try outbox.statistics()
        return AttributionFlushResult(attempted: records.count, acknowledged: acknowledged, remaining: statistics.count)
    }

    public func diagnostics() throws -> AttributionDiagnostics {
        let statistics = try outbox.statistics()
        return AttributionDiagnostics(
            runtimeVersion: AttributionCore.version,
            manifestVersion: manifest.manifestVersion,
            appInstanceId: try stateString("app_instance_id"),
            queuedRecordCount: statistics.count,
            oldestQueuedAt: statistics.oldest,
            lastFlushAt: try stateString("last_flush_at").flatMap(Self.date),
            lastFlushErrorCode: try stateString("last_flush_error").flatMap { $0.isEmpty ? nil : $0 },
            lastDeepLink: try stateString("last_deep_link").flatMap(URL.init(string:)),
            adServicesCollection: (try stateString("adservices_status")) ?? "not_attempted",
            conversionAuthority: manifest.conversionAuthority
        )
    }

    private func installationIdentifier(now: Date) throws -> String {
        if let existing = try stateString("app_instance_id") { return existing }
        let identifier = UUID().uuidString.lowercased()
        let firstOpenId = UUID().uuidString.lowercased()
        let payload = try JSONSerialization.data(withJSONObject: [:], options: [.sortedKeys])
        let firstOpen = record(
            id: firstOpenId,
            kind: .event,
            event: "first_open",
            payload: payload,
            occurredAt: now
        )
        _ = try outbox.initializeInstallation(appInstanceId: identifier, firstOpen: firstOpen)
        return try requireStateString("app_instance_id")
    }

    private func enqueue(
        event: String,
        properties: [String: AttributionJSONValue],
        kind: AttributionOutboxRecord.Kind,
        occurredAt: Date
    ) throws {
        let payload = try JSONEncoder.attribution.encode(properties)
        try outbox.insert(record(
            id: UUID().uuidString.lowercased(),
            kind: kind,
            event: event,
            payload: payload,
            occurredAt: occurredAt
        ))
        try outbox.prune(now: occurredAt, maximumRecords: Self.maximumQueueSize)
    }

    private func record(
        id: String,
        kind: AttributionOutboxRecord.Kind,
        event: String,
        payload: Data,
        occurredAt: Date
    ) -> AttributionOutboxRecord {
        AttributionOutboxRecord(
            id: id,
            kind: kind,
            eventName: event,
            occurredAt: occurredAt,
            payload: payload,
            appVersion: bundle.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "unknown",
            appBuild: bundle.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "unknown",
            sdkVersion: AttributionCore.version,
            releaseManifestVersion: manifest.manifestVersion,
            conversionSchemaVersion: manifest.conversionSchemaVersion,
            attemptCount: 0,
            nextAttemptAt: occurredAt,
            expiresAt: occurredAt.addingTimeInterval(Self.retention)
        )
    }

    private func captureDeepLink(_ url: URL, now: Date) throws {
        guard url.scheme != nil else { return }
        let redacted = URLComponents(url: url, resolvingAgainstBaseURL: false).map { components -> String in
            var clean = components
            clean.user = nil
            clean.password = nil
            clean.query = nil
            clean.fragment = nil
            return clean.url?.absoluteString ?? "invalid"
        } ?? "invalid"
        try outbox.setState("last_deep_link", value: Data(redacted.utf8))
        try enqueue(
            event: "deep_link_open",
            properties: ["url": .string(redacted)],
            kind: .diagnostic,
            occurredAt: now
        )
    }

    private func collectAdServicesTokenIfNeeded(now: Date) throws {
        if try stateString("adservices_collected_at") != nil { return }
        #if os(iOS) && !targetEnvironment(simulator) && canImport(AdServices)
        do {
            let token = try AAAttribution.attributionToken()
            let record = record(
                id: UUID().uuidString.lowercased(),
                kind: .adServicesToken,
                event: "adservices_token",
                payload: Data(token.utf8),
                occurredAt: now
            )
            try outbox.insert(record)
            try outbox.setState("adservices_collected_at", value: Data(Self.timestamp(now).utf8))
            try outbox.setState("adservices_status", value: Data("queued".utf8))
        } catch {
            try outbox.setState("adservices_status", value: Data("unavailable".utf8))
        }
        #else
        try outbox.setState("adservices_status", value: Data("unsupported_runtime".utf8))
        #endif
    }

    private func appInstanceToken(appInstanceId: String) async throws -> String {
        if let token = try stateString("app_instance_token"),
           let expiration = try stateString("app_instance_token_expires_at").flatMap(Self.date),
           expiration.timeIntervalSinceNow > 24 * 60 * 60 {
            return token
        }
        let registrationId = UUID().uuidString.lowercased()
        let response = try await transport.register(
            appId: manifest.appId,
            appInstanceId: appInstanceId,
            registrationId: registrationId,
            origin: manifest.collectorOrigin
        )
        try outbox.setState("app_instance_token", value: Data(response.accessToken.utf8))
        try outbox.setState("app_instance_token_expires_at", value: Data(response.expiresAt.utf8))
        return response.accessToken
    }

    private func validate(
        properties: [String: AttributionJSONValue],
        for event: AttributionEventDefinition
    ) throws {
        for key in properties.keys where event.properties[key] == nil {
            throw AttributionRuntimeError.undeclaredProperty(event: event.name, property: key)
        }
        for (key, definition) in event.properties {
            guard let value = properties[key] else {
                if definition.required { throw AttributionRuntimeError.missingProperty(event: event.name, property: key) }
                continue
            }
            let valid: Bool
            switch (definition.type, value) {
            case (.string, .string), (.number, .number), (.boolean, .bool): valid = true
            default: valid = false
            }
            if !valid { throw AttributionRuntimeError.invalidPropertyType(event: event.name, property: key) }
        }
    }

    private func updateConversions(
        fine: Int,
        coarse: AttributionCoarseValue?,
        lock: Bool
    ) async throws {
        for driver in manifest.enabledAppleDrivers {
            if let state = try outbox.conversionState(driver: driver, window: 0) {
                if state.locked || fine < state.fine { continue }
                if fine == state.fine, state.coarse == coarse?.rawValue, state.locked == lock { continue }
            }
            let result = await AttributionCore.update(
                fineConversionValue: fine,
                coarseConversionValue: coarse,
                lockPostback: lock,
                driver: driver
            )
            if result.status == .succeeded {
                try outbox.setConversionState(
                    driver: driver,
                    window: 0,
                    fine: fine,
                    coarse: coarse?.rawValue,
                    locked: lock
                )
            }
        }
    }

    private func stateString(_ key: String) throws -> String? {
        guard let data = try outbox.state(key) else { return nil }
        return String(data: data, encoding: .utf8)
    }

    private func requireStateString(_ key: String) throws -> String {
        guard let value = try stateString(key), !value.isEmpty else {
            throw AttributionRuntimeError.queueUnavailable
        }
        return value
    }

    private static func timestamp(_ date: Date) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter.string(from: date)
    }

    private static func date(_ value: String) -> Date? {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter.date(from: value)
    }
}

public enum Attribution {
    private static let coordinator = AttributionCoordinator()

    public static func ready() async throws {
        try await coordinator.client().ready()
    }

    public static func track(
        _ event: String,
        properties: [String: AttributionJSONValue] = [:],
        consentState: AttributionConsentState = .unknown
    ) async throws {
        try await coordinator.client().track(event, properties: properties, consentState: consentState)
    }

    public static func captureDeepLink(_ url: URL) async throws {
        try await coordinator.client().captureDeepLink(url)
    }

    public static func flush() async throws -> AttributionFlushResult {
        try await coordinator.client().flush()
    }

    public static func getDiagnostics() async throws -> AttributionDiagnostics {
        try await coordinator.client().diagnostics()
    }
}

private actor AttributionCoordinator {
    private var cached: AttributionClient?

    func client() throws -> AttributionClient {
        if let cached { return cached }
        let manifest = try Self.loadManifest()
        let directory = try FileManager.default.url(
            for: .applicationSupportDirectory,
            in: .userDomainMask,
            appropriateFor: nil,
            create: true
        ).appendingPathComponent("AttributionKit", isDirectory: true)
        let client = try AttributionClient(
            manifest: manifest,
            storageURL: directory.appendingPathComponent("outbox.sqlite3")
        )
        cached = client
        return client
    }

    private static func loadManifest(bundle: Bundle = .main) throws -> AttributionReleaseManifest {
        if let inline = bundle.object(forInfoDictionaryKey: "AttributionKitReleaseManifestJSON") as? String,
           let data = inline.data(using: .utf8) {
            return try AttributionReleaseManifest.decode(data)
        }
        let resource = bundle.object(forInfoDictionaryKey: "AttributionKitReleaseManifest") as? String
            ?? "AttributionKitReleaseManifest"
        let name = (resource as NSString).deletingPathExtension
        let ext = (resource as NSString).pathExtension.isEmpty ? "json" : (resource as NSString).pathExtension
        guard let url = bundle.url(forResource: name, withExtension: ext) else {
            throw AttributionRuntimeError.manifestNotFound
        }
        return try AttributionReleaseManifest.decode(Data(contentsOf: url))
    }
}

private extension JSONEncoder {
    static var attribution: JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        return encoder
    }
}
