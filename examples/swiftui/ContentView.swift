import AttributionCore
import SwiftUI

struct ContentView: View {
    @State private var status = "Not recorded"

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
                        let report = try await AttributionCore.record("install")
                        status = "AAK \(report.adAttributionKit.status.rawValue) · SKAN \(report.skAdNetwork.status.rawValue)"
                    } catch {
                        status = error.localizedDescription
                    }
                }
            }
            .accessibilityIdentifier("record-install")
            Text(status)
                .accessibilityIdentifier("last-result")
        }
        .padding()
    }
}
