import CryptoKit
import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

protocol AttributionTransporting: Sendable {
    func register(appId: String, appInstanceId: String, registrationId: String, origin: URL) async throws -> AttributionRegistrationResponse
    func send(record: AttributionOutboxRecord, appId: String, appInstanceId: String, token: String, origin: URL, eventSchemaVersion: String) async throws
}

struct AttributionRegistrationResponse: Codable, Sendable {
    let accessToken: String
    let tokenType: String
    let expiresAt: String
    let trustState: String
    let duplicate: Bool
}

struct AttributionHTTPTransport: AttributionTransporting {
    private let session: URLSession

    init(session: URLSession? = nil) {
        if let session {
            self.session = session
        } else {
            let configuration = URLSessionConfiguration.ephemeral
            configuration.timeoutIntervalForRequest = 15
            configuration.timeoutIntervalForResource = 30
            configuration.requestCachePolicy = .reloadIgnoringLocalCacheData
            configuration.urlCache = nil
            configuration.httpCookieStorage = nil
            configuration.httpShouldSetCookies = false
            configuration.waitsForConnectivity = false
            self.session = URLSession(configuration: configuration)
        }
    }

    func register(
        appId: String,
        appInstanceId: String,
        registrationId: String,
        origin: URL
    ) async throws -> AttributionRegistrationResponse {
        let body: [String: Any] = [
            "appId": appId,
            "appInstanceId": appInstanceId,
            "attestation": ["mode": "none"],
            "registrationId": registrationId,
            "schemaVersion": "app_instance_registration_1",
        ]
        let data = try JSONSerialization.data(withJSONObject: body, options: [.sortedKeys, .withoutEscapingSlashes])
        let url = origin
            .appendingPathComponent("v1")
            .appendingPathComponent("applications")
            .appendingPathComponent(appId)
            .appendingPathComponent("app-instances")
            .appendingPathComponent("registrations")
        let response = try await post(url: url, data: data, bearer: nil)
        guard response.statusCode == 200 || response.statusCode == 201 else {
            throw AttributionRuntimeError.transport(response.statusCode)
        }
        return try JSONDecoder().decode(AttributionRegistrationResponse.self, from: response.data)
    }

    func send(
        record: AttributionOutboxRecord,
        appId: String,
        appInstanceId: String,
        token: String,
        origin: URL,
        eventSchemaVersion: String
    ) async throws {
        let url: URL
        let data: Data
        switch record.kind {
        case .event, .diagnostic:
            let properties = try JSONSerialization.jsonObject(with: record.payload)
            let body: [String: Any] = [
                "appId": appId,
                "appInstanceId": appInstanceId,
                "consentState": "unknown",
                "eventId": record.id,
                "eventName": record.eventName,
                "occurredAt": Self.timestamp(record.occurredAt),
                "properties": properties,
                "schemaVersion": eventSchemaVersion,
            ]
            data = try JSONSerialization.data(withJSONObject: body, options: [.sortedKeys, .withoutEscapingSlashes])
            url = origin
                .appendingPathComponent("v1")
                .appendingPathComponent("applications")
                .appendingPathComponent(appId)
                .appendingPathComponent("events")
        case .adServicesToken:
            let body: [String: Any] = [
                "appId": appId,
                "appInstanceId": appInstanceId,
                "claimId": record.id,
                "collectedAt": Self.timestamp(record.occurredAt),
                "schemaVersion": "adservices_claim_1",
                "token": String(decoding: record.payload, as: UTF8.self),
            ]
            data = try JSONSerialization.data(withJSONObject: body, options: [.sortedKeys, .withoutEscapingSlashes])
            url = origin
                .appendingPathComponent("v1")
                .appendingPathComponent("applications")
                .appendingPathComponent(appId)
                .appendingPathComponent("adservices-claims")
        }
        let response = try await post(url: url, data: data, bearer: token)
        guard (200...299).contains(response.statusCode) else {
            throw AttributionRuntimeError.transport(response.statusCode)
        }
    }

    private func post(url: URL, data: Data, bearer: String?) async throws -> (data: Data, statusCode: Int) {
        let digest = SHA256.hash(data: data)
        let digestData = Data(digest)
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.httpBody = data
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("sha-256=:\(digestData.base64EncodedString()):", forHTTPHeaderField: "Content-Digest")
        request.setValue(digestData.map { String(format: "%02x", $0) }.joined(), forHTTPHeaderField: "Idempotency-Key")
        request.setValue("no-store", forHTTPHeaderField: "Cache-Control")
        if let bearer { request.setValue("Bearer \(bearer)", forHTTPHeaderField: "Authorization") }
        let (responseData, rawResponse) = try await session.data(for: request)
        guard let response = rawResponse as? HTTPURLResponse else {
            throw AttributionRuntimeError.transport(0)
        }
        return (responseData, response.statusCode)
    }

    private static func timestamp(_ date: Date) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter.string(from: date)
    }
}
