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
}
