// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "AttributionKit",
    platforms: [
        .iOS(.v15),
        .macOS(.v13),
    ],
    products: [
        .library(name: "AttributionCore", targets: ["AttributionCore"]),
    ],
    targets: [
        .target(
            name: "AttributionCore",
            path: "native/AttributionCore/Sources/AttributionCore",
            resources: [.process("PrivacyInfo.xcprivacy")]
        ),
        .testTarget(
            name: "AttributionCoreTests",
            dependencies: ["AttributionCore"],
            path: "native/AttributionCore/Tests/AttributionCoreTests"
        ),
    ]
)
