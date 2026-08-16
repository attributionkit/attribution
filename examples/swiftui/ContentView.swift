import AttributionCore
import Foundation
import SwiftUI

struct ContentView: View {
    @State private var status = "Not recorded"
    @State private var probeJSON = ""

    var body: some View {
        VStack(spacing: 12) {
            Image(systemName: "checkmark.shield")
                .font(.system(size: 48))
            Text("AttributionKit SwiftUI Probe")
                .font(.title2.bold())
                .accessibilityAddTraits(.isHeader)
            Text("Runtime \(AttributionCore.version)")
            Button("Record Install") {
                Task {
                    do {
                        let report = try await AttributionKitGeneratedPlan.record("install")
                        status = "AAK \(report.adAttributionKit.status.rawValue) · SKAN \(report.skAdNetwork.status.rawValue)"
                        let data = try JSONEncoder().encode(report)
                        guard let json = String(data: data, encoding: .utf8) else {
                            throw CocoaError(.fileWriteInapplicableStringEncoding)
                        }
                        probeJSON = json
                        // One exact JSON line for `attribution probe import`.
                        print(json)
                    } catch {
                        status = error.localizedDescription
                    }
                }
            }
            .accessibilityIdentifier("record-install")
            Text(status)
                .accessibilityIdentifier("last-result")
            if !probeJSON.isEmpty {
                Text(probeJSON)
                    .font(.caption.monospaced())
                    .textSelection(.enabled)
                    .accessibilityIdentifier("copyable-probe-report")
            }
        }
        .padding()
    }
}
