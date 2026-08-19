import XCTest
@testable import AttributionCore

final class AttributionCoreTests: XCTestCase {
    private let schemaDigest = String(repeating: "a", count: 64)

    func testEventOrderDefinesFineConversionValue() throws {
        let config = try AttributionConfiguration(
            schemaHash: schemaDigest,
            events: ["install", "trial", "purchase", "retention"]
        )

        XCTAssertEqual(try config.conversionValue(for: "install"), 0)
        XCTAssertEqual(try config.conversionValue(for: "purchase"), 2)
    }

    func testDuplicateEventsAreRejected() {
        XCTAssertThrowsError(
            try AttributionConfiguration(schemaHash: schemaDigest, events: ["install", "install"])
        ) { error in
            XCTAssertEqual(error as? AttributionCoreError, .duplicateEvent("install"))
        }
    }

    func testUnknownEventIsRejected() throws {
        let config = try AttributionConfiguration(schemaHash: schemaDigest, events: ["install"])
        XCTAssertThrowsError(try config.conversionValue(for: "purchase")) { error in
            XCTAssertEqual(error as? AttributionCoreError, .unknownEvent("purchase"))
        }
    }

    func testOutOfRangeValueIsRejected() {
        XCTAssertThrowsError(
            try AttributionConfiguration(schemaHash: schemaDigest, eventValues: ["purchase": 64])
        ) { error in
            XCTAssertEqual(
                error as? AttributionCoreError,
                .invalidConversionValue(event: "purchase", value: 64)
            )
        }
    }

    func testMalformedSchemaHashIsRejected() {
        XCTAssertThrowsError(
            try AttributionConfiguration(schemaHash: "not-a-sha", events: ["install"])
        ) { error in
            XCTAssertEqual(error as? AttributionCoreError, .invalidSchemaHash)
        }
    }

    func testDuplicateConversionValuesAreRejected() {
        XCTAssertThrowsError(
            try AttributionConfiguration(schemaHash: schemaDigest, eventValues: ["install": 0, "trial": 0])
        ) { error in
            XCTAssertEqual(error as? AttributionCoreError, .duplicateConversionValue(0))
        }
    }

    func testBundleValuesRejectBooleanAndFractionalNumbers() {
        XCTAssertThrowsError(try AttributionConfiguration.parseEventValues(["install": true]))
        XCTAssertThrowsError(try AttributionConfiguration.parseEventValues(["install": 2.5]))
        XCTAssertEqual(
            try AttributionConfiguration.parseEventValues(["install": 2]),
            ["install": 2]
        )
    }

    func testRecordReportsUnavailableBackendsOffDevice() async throws {
        #if os(macOS)
        let config = try AttributionConfiguration(schemaHash: schemaDigest, events: ["install"])
        let report = try await AttributionCore.record("install", configuration: config)
        XCTAssertEqual(report.fineConversionValue, 0)
        XCTAssertEqual(report.adAttributionKit.status, .unavailable)
        XCTAssertEqual(report.skAdNetwork.status, .unavailable)
        #endif
    }

    func testReleaseManifestRejectsMalformedDigestAndUndeclaredProperty() async throws {
        XCTAssertThrowsError(try makeManifest(configurationDigest: "sha256:" + String(repeating: "Z", count: 64)))

        let storage = temporaryDatabaseURL()
        let transport = RecordingTransport()
        let client = try AttributionClient(
            manifest: makeManifest(),
            storageURL: storage,
            bundle: .main,
            transport: transport
        )
        try await client.ready()
        do {
            try await client.track("purchase", properties: ["email": .string("private@example.com")])
            XCTFail("expected undeclared property rejection")
        } catch {
            XCTAssertEqual(error as? AttributionRuntimeError, .undeclaredProperty(event: "purchase", property: "email"))
        }
    }

    func testInstallationAndFirstOpenAreAtomicAndStable() throws {
        let outbox = try AttributionOutbox(path: temporaryDatabaseURL().path)
        let now = Date()
        let first = outboxRecord(id: "11111111-1111-4111-8111-111111111111", now: now)
        XCTAssertTrue(try outbox.initializeInstallation(appInstanceId: "22222222-2222-4222-8222-222222222222", firstOpen: first))
        XCTAssertFalse(try outbox.initializeInstallation(appInstanceId: "33333333-3333-4333-8333-333333333333", firstOpen: outboxRecord(id: "44444444-4444-4444-8444-444444444444", now: now)))
        XCTAssertEqual(String(data: try XCTUnwrap(outbox.state("app_instance_id")), encoding: .utf8), "22222222-2222-4222-8222-222222222222")
        XCTAssertEqual(try outbox.statistics().count, 1)
    }

    func testReadyDeliversFirstOpenOnlyOnceAcrossClientRecreation() async throws {
        let storage = temporaryDatabaseURL()
        let transport = RecordingTransport()
        let first = try AttributionClient(manifest: makeManifest(), storageURL: storage, bundle: .main, transport: transport)
        try await first.ready()
        let firstEvents = await transport.events()
        XCTAssertEqual(firstEvents, ["first_open"])

        let second = try AttributionClient(manifest: makeManifest(), storageURL: storage, bundle: .main, transport: transport)
        try await second.ready()
        let secondEvents = await transport.events()
        let diagnostics = try await second.diagnostics()
        XCTAssertEqual(secondEvents, ["first_open"])
        XCTAssertEqual(diagnostics.queuedRecordCount, 0)
    }

    private func makeManifest(configurationDigest: String? = nil) throws -> AttributionReleaseManifest {
        try AttributionReleaseManifest(
            manifestVersion: "1",
            appId: "11111111-1111-4111-8111-111111111111",
            bundleId: Bundle.main.bundleIdentifier ?? "sh.attribution.fixture",
            environment: "test",
            collectorOrigin: XCTUnwrap(URL(string: "https://api.attribution.test/")),
            conversionAuthority: .none,
            conversionSchemaVersion: "apple_conversion_1",
            eventSchemaVersion: "events_4",
            enabledAppleDrivers: [],
            associatedDomains: [],
            configurationDigest: configurationDigest ?? "sha256:" + String(repeating: "a", count: 64),
            events: [AttributionEventDefinition(name: "purchase")]
        )
    }

    private func temporaryDatabaseURL() -> URL {
        let url = FileManager.default.temporaryDirectory
            .appendingPathComponent(UUID().uuidString, isDirectory: true)
            .appendingPathComponent("outbox.sqlite3")
        try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        return url
    }

    private func outboxRecord(id: String, now: Date) -> AttributionOutboxRecord {
        AttributionOutboxRecord(
            id: id,
            kind: .event,
            eventName: "first_open",
            occurredAt: now,
            payload: Data("{}".utf8),
            appVersion: "1",
            appBuild: "1",
            sdkVersion: AttributionCore.version,
            releaseManifestVersion: "1",
            conversionSchemaVersion: "apple_conversion_1",
            attemptCount: 0,
            nextAttemptAt: now,
            expiresAt: now.addingTimeInterval(60)
        )
    }
}

private actor RecordingTransport: AttributionTransporting {
    private var delivered: [String] = []

    func register(appId: String, appInstanceId: String, registrationId: String, origin: URL) async throws -> AttributionRegistrationResponse {
        AttributionRegistrationResponse(
            accessToken: "test-token",
            tokenType: "Bearer",
            expiresAt: "2099-01-01T00:00:00.000Z",
            trustState: "unverified",
            duplicate: false
        )
    }

    func send(record: AttributionOutboxRecord, appId: String, appInstanceId: String, token: String, origin: URL, eventSchemaVersion: String) async throws {
        delivered.append(record.eventName)
    }

    func events() -> [String] { delivered }
}
