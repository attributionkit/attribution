import Foundation
import SQLite3

struct AttributionOutboxRecord: Equatable, Sendable {
    enum Kind: String, Sendable {
        case event
        case adServicesToken = "adservices_token"
        case diagnostic
    }

    let id: String
    let kind: Kind
    let eventName: String
    let occurredAt: Date
    let payload: Data
    let appVersion: String
    let appBuild: String
    let sdkVersion: String
    let releaseManifestVersion: String
    let conversionSchemaVersion: String
    let attemptCount: Int
    let nextAttemptAt: Date
    let expiresAt: Date
}

final class AttributionOutbox: @unchecked Sendable {
    private let database: OpaquePointer
    private let transient = unsafeBitCast(-1, to: sqlite3_destructor_type.self)

    init(path: String) throws {
        var handle: OpaquePointer?
        let flags = SQLITE_OPEN_CREATE | SQLITE_OPEN_READWRITE | SQLITE_OPEN_FULLMUTEX
        guard sqlite3_open_v2(path, &handle, flags, nil) == SQLITE_OK, let handle else {
            if let handle { sqlite3_close(handle) }
            throw AttributionRuntimeError.queueUnavailable
        }
        database = handle
        do {
            try execute("PRAGMA journal_mode=WAL")
            try execute("PRAGMA synchronous=FULL")
            try execute("PRAGMA foreign_keys=ON")
            try execute("""
                CREATE TABLE IF NOT EXISTS runtime_state (
                  key TEXT PRIMARY KEY NOT NULL,
                  value BLOB NOT NULL
                ) WITHOUT ROWID
                """)
            try execute("""
                CREATE TABLE IF NOT EXISTS outbox (
                  client_event_id TEXT PRIMARY KEY NOT NULL,
                  kind TEXT NOT NULL,
                  event_name TEXT NOT NULL,
                  occurred_at_ms INTEGER NOT NULL,
                  payload BLOB NOT NULL,
                  app_version TEXT NOT NULL,
                  app_build TEXT NOT NULL,
                  sdk_version TEXT NOT NULL,
                  release_manifest_version TEXT NOT NULL,
                  conversion_schema_version TEXT NOT NULL,
                  attempt_count INTEGER NOT NULL DEFAULT 0,
                  next_attempt_at_ms INTEGER NOT NULL,
                  expires_at_ms INTEGER NOT NULL,
                  created_at_ms INTEGER NOT NULL
                ) WITHOUT ROWID
                """)
            try execute("CREATE INDEX IF NOT EXISTS outbox_due ON outbox(next_attempt_at_ms, occurred_at_ms)")
            try execute("""
                CREATE TABLE IF NOT EXISTS conversion_state (
                  driver TEXT NOT NULL,
                  window_index INTEGER NOT NULL,
                  fine_value INTEGER NOT NULL,
                  coarse_value TEXT,
                  locked INTEGER NOT NULL,
                  updated_at_ms INTEGER NOT NULL,
                  PRIMARY KEY(driver, window_index)
                ) WITHOUT ROWID
                """)
        } catch {
            sqlite3_close(handle)
            throw error
        }
    }

    deinit {
        sqlite3_close(database)
    }

    func state(_ key: String) throws -> Data? {
        let statement = try prepare("SELECT value FROM runtime_state WHERE key = ?1")
        defer { sqlite3_finalize(statement) }
        try bind(key, at: 1, to: statement)
        guard sqlite3_step(statement) == SQLITE_ROW else { return nil }
        return data(statement, column: 0)
    }

    func setState(_ key: String, value: Data) throws {
        let statement = try prepare("""
            INSERT INTO runtime_state(key, value) VALUES (?1, ?2)
            ON CONFLICT(key) DO UPDATE SET value = excluded.value
            """)
        defer { sqlite3_finalize(statement) }
        try bind(key, at: 1, to: statement)
        try bind(value, at: 2, to: statement)
        try stepDone(statement)
    }

    func initializeInstallation(appInstanceId: String, firstOpen: AttributionOutboxRecord) throws -> Bool {
        try execute("BEGIN IMMEDIATE")
        do {
            if try state("app_instance_id") != nil {
                try execute("COMMIT")
                return false
            }
            try setState("app_instance_id", value: Data(appInstanceId.utf8))
            try setState("first_open_event_id", value: Data(firstOpen.id.utf8))
            try insert(firstOpen)
            try execute("COMMIT")
            return true
        } catch {
            try? execute("ROLLBACK")
            throw error
        }
    }

    func insert(_ record: AttributionOutboxRecord) throws {
        let statement = try prepare("""
            INSERT OR IGNORE INTO outbox (
              client_event_id, kind, event_name, occurred_at_ms, payload,
              app_version, app_build, sdk_version, release_manifest_version,
              conversion_schema_version, attempt_count, next_attempt_at_ms,
              expires_at_ms, created_at_ms
            ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14)
            """)
        defer { sqlite3_finalize(statement) }
        try bind(record.id, at: 1, to: statement)
        try bind(record.kind.rawValue, at: 2, to: statement)
        try bind(record.eventName, at: 3, to: statement)
        sqlite3_bind_int64(statement, 4, milliseconds(record.occurredAt))
        try bind(record.payload, at: 5, to: statement)
        try bind(record.appVersion, at: 6, to: statement)
        try bind(record.appBuild, at: 7, to: statement)
        try bind(record.sdkVersion, at: 8, to: statement)
        try bind(record.releaseManifestVersion, at: 9, to: statement)
        try bind(record.conversionSchemaVersion, at: 10, to: statement)
        sqlite3_bind_int(statement, 11, Int32(record.attemptCount))
        sqlite3_bind_int64(statement, 12, milliseconds(record.nextAttemptAt))
        sqlite3_bind_int64(statement, 13, milliseconds(record.expiresAt))
        sqlite3_bind_int64(statement, 14, milliseconds(Date()))
        try stepDone(statement)
    }

    func due(limit: Int, now: Date) throws -> [AttributionOutboxRecord] {
        let statement = try prepare("""
            SELECT client_event_id, kind, event_name, occurred_at_ms, payload,
                   app_version, app_build, sdk_version, release_manifest_version,
                   conversion_schema_version, attempt_count, next_attempt_at_ms,
                   expires_at_ms
            FROM outbox
            WHERE next_attempt_at_ms <= ?1 AND expires_at_ms > ?1
            ORDER BY occurred_at_ms, client_event_id
            LIMIT ?2
            """)
        defer { sqlite3_finalize(statement) }
        sqlite3_bind_int64(statement, 1, milliseconds(now))
        sqlite3_bind_int(statement, 2, Int32(max(1, min(limit, 100))))
        var records: [AttributionOutboxRecord] = []
        while sqlite3_step(statement) == SQLITE_ROW {
            guard let kind = AttributionOutboxRecord.Kind(rawValue: text(statement, column: 1)) else {
                continue
            }
            records.append(AttributionOutboxRecord(
                id: text(statement, column: 0),
                kind: kind,
                eventName: text(statement, column: 2),
                occurredAt: date(statement, column: 3),
                payload: data(statement, column: 4),
                appVersion: text(statement, column: 5),
                appBuild: text(statement, column: 6),
                sdkVersion: text(statement, column: 7),
                releaseManifestVersion: text(statement, column: 8),
                conversionSchemaVersion: text(statement, column: 9),
                attemptCount: Int(sqlite3_column_int(statement, 10)),
                nextAttemptAt: date(statement, column: 11),
                expiresAt: date(statement, column: 12)
            ))
        }
        return records
    }

    func acknowledge(_ id: String) throws {
        let statement = try prepare("DELETE FROM outbox WHERE client_event_id = ?1")
        defer { sqlite3_finalize(statement) }
        try bind(id, at: 1, to: statement)
        try stepDone(statement)
    }

    func reschedule(_ id: String, attemptCount: Int, nextAttemptAt: Date) throws {
        let statement = try prepare("""
            UPDATE outbox SET attempt_count = ?2, next_attempt_at_ms = ?3
            WHERE client_event_id = ?1
            """)
        defer { sqlite3_finalize(statement) }
        try bind(id, at: 1, to: statement)
        sqlite3_bind_int(statement, 2, Int32(attemptCount))
        sqlite3_bind_int64(statement, 3, milliseconds(nextAttemptAt))
        try stepDone(statement)
    }

    func prune(now: Date, maximumRecords: Int) throws {
        let nowMs = milliseconds(now)
        let expired = try prepare("DELETE FROM outbox WHERE expires_at_ms <= ?1")
        defer { sqlite3_finalize(expired) }
        sqlite3_bind_int64(expired, 1, nowMs)
        try stepDone(expired)

        let overflow = try prepare("""
            DELETE FROM outbox WHERE client_event_id IN (
              SELECT client_event_id FROM outbox
              ORDER BY occurred_at_ms DESC, client_event_id DESC
              LIMIT -1 OFFSET ?1
            )
            """)
        defer { sqlite3_finalize(overflow) }
        sqlite3_bind_int(overflow, 1, Int32(max(1, maximumRecords)))
        try stepDone(overflow)
    }

    func statistics() throws -> (count: Int, oldest: Date?) {
        let statement = try prepare("SELECT count(*), min(occurred_at_ms) FROM outbox")
        defer { sqlite3_finalize(statement) }
        guard sqlite3_step(statement) == SQLITE_ROW else { return (0, nil) }
        let count = Int(sqlite3_column_int64(statement, 0))
        let oldest = sqlite3_column_type(statement, 1) == SQLITE_NULL ? nil : date(statement, column: 1)
        return (count, oldest)
    }

    func conversionState(driver: AttributionAppleDriver, window: Int) throws -> (fine: Int, coarse: String?, locked: Bool)? {
        let statement = try prepare("""
            SELECT fine_value, coarse_value, locked FROM conversion_state
            WHERE driver = ?1 AND window_index = ?2
            """)
        defer { sqlite3_finalize(statement) }
        try bind(driver.rawValue, at: 1, to: statement)
        sqlite3_bind_int(statement, 2, Int32(window))
        guard sqlite3_step(statement) == SQLITE_ROW else { return nil }
        let coarse = sqlite3_column_type(statement, 1) == SQLITE_NULL ? nil : text(statement, column: 1)
        return (Int(sqlite3_column_int(statement, 0)), coarse, sqlite3_column_int(statement, 2) == 1)
    }

    func setConversionState(driver: AttributionAppleDriver, window: Int, fine: Int, coarse: String?, locked: Bool) throws {
        let statement = try prepare("""
            INSERT INTO conversion_state(driver, window_index, fine_value, coarse_value, locked, updated_at_ms)
            VALUES (?1, ?2, ?3, ?4, ?5, ?6)
            ON CONFLICT(driver, window_index) DO UPDATE SET
              fine_value = excluded.fine_value,
              coarse_value = excluded.coarse_value,
              locked = excluded.locked,
              updated_at_ms = excluded.updated_at_ms
            """)
        defer { sqlite3_finalize(statement) }
        try bind(driver.rawValue, at: 1, to: statement)
        sqlite3_bind_int(statement, 2, Int32(window))
        sqlite3_bind_int(statement, 3, Int32(fine))
        if let coarse { try bind(coarse, at: 4, to: statement) } else { sqlite3_bind_null(statement, 4) }
        sqlite3_bind_int(statement, 5, locked ? 1 : 0)
        sqlite3_bind_int64(statement, 6, milliseconds(Date()))
        try stepDone(statement)
    }

    private func execute(_ sql: String) throws {
        guard sqlite3_exec(database, sql, nil, nil, nil) == SQLITE_OK else {
            throw AttributionRuntimeError.queueUnavailable
        }
    }

    private func prepare(_ sql: String) throws -> OpaquePointer {
        var statement: OpaquePointer?
        guard sqlite3_prepare_v2(database, sql, -1, &statement, nil) == SQLITE_OK, let statement else {
            throw AttributionRuntimeError.queueUnavailable
        }
        return statement
    }

    private func bind(_ value: String, at index: Int32, to statement: OpaquePointer) throws {
        guard sqlite3_bind_text(statement, index, value, -1, transient) == SQLITE_OK else {
            throw AttributionRuntimeError.queueUnavailable
        }
    }

    private func bind(_ value: Data, at index: Int32, to statement: OpaquePointer) throws {
        let result = value.withUnsafeBytes { bytes in
            sqlite3_bind_blob(statement, index, bytes.baseAddress, Int32(bytes.count), transient)
        }
        guard result == SQLITE_OK else { throw AttributionRuntimeError.queueUnavailable }
    }

    private func stepDone(_ statement: OpaquePointer) throws {
        guard sqlite3_step(statement) == SQLITE_DONE else {
            throw AttributionRuntimeError.queueUnavailable
        }
    }

    private func text(_ statement: OpaquePointer, column: Int32) -> String {
        guard let raw = sqlite3_column_text(statement, column) else { return "" }
        return String(cString: raw)
    }

    private func data(_ statement: OpaquePointer, column: Int32) -> Data {
        let count = Int(sqlite3_column_bytes(statement, column))
        guard count > 0, let bytes = sqlite3_column_blob(statement, column) else { return Data() }
        return Data(bytes: bytes, count: count)
    }

    private func date(_ statement: OpaquePointer, column: Int32) -> Date {
        Date(timeIntervalSince1970: Double(sqlite3_column_int64(statement, column)) / 1_000)
    }

    private func milliseconds(_ date: Date) -> Int64 {
        Int64((date.timeIntervalSince1970 * 1_000).rounded())
    }
}
