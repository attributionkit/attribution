// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "AttributionCore",
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
            resources: [.process("PrivacyInfo.xcprivacy")]
        ),
        .testTarget(
            name: "AttributionCoreTests",
            dependencies: ["AttributionCore"]
        ),
    ]
)
