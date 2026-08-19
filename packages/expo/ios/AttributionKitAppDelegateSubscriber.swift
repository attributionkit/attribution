import ExpoModulesCore
import Foundation
import UIKit

public final class AttributionKitAppDelegateSubscriber: ExpoAppDelegateSubscriber {
    public func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        let launchURL = launchOptions?[.url] as? URL
        Task {
            try? await Attribution.ready()
            if let launchURL { try? await Attribution.captureDeepLink(launchURL) }
        }
        return false
    }

    public func application(
        _ app: UIApplication,
        open url: URL,
        options: [UIApplication.OpenURLOptionsKey: Any] = [:]
    ) -> Bool {
        Task { try? await Attribution.captureDeepLink(url) }
        return false
    }

    public func application(
        _ application: UIApplication,
        continue userActivity: NSUserActivity,
        restorationHandler: @escaping ([UIUserActivityRestoring]?) -> Void
    ) -> Bool {
        guard let url = userActivity.webpageURL else { return false }
        Task { try? await Attribution.captureDeepLink(url) }
        return false
    }

    public func applicationDidBecomeActive(_ application: UIApplication) {
        Task { _ = try? await Attribution.flush() }
    }

    public func applicationDidEnterBackground(_ application: UIApplication) {
        var task = UIBackgroundTaskIdentifier.invalid
        task = application.beginBackgroundTask(withName: "AttributionKit flush") {
            if task != .invalid { application.endBackgroundTask(task) }
            task = .invalid
        }
        Task {
            _ = try? await Attribution.flush()
            if task != .invalid { application.endBackgroundTask(task) }
            task = .invalid
        }
    }
}
