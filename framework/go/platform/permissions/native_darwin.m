#import <ApplicationServices/ApplicationServices.h>
#import <AVFoundation/AVFoundation.h>
#import <CoreGraphics/CoreGraphics.h>
#import <Foundation/Foundation.h>
#import <UserNotifications/UserNotifications.h>

enum {
  GUIPermissionAuthorized = 0,
  GUIPermissionNotDetermined = 1,
  GUIPermissionDenied = 2,
  GUIPermissionRestricted = 3,
  GUIPermissionUnavailable = 4,
};

int gui_permission_microphone_status(void) {
  @autoreleasepool {
    switch ([AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio]) {
      case AVAuthorizationStatusAuthorized:
        return GUIPermissionAuthorized;
      case AVAuthorizationStatusNotDetermined:
        return GUIPermissionNotDetermined;
      case AVAuthorizationStatusDenied:
        return GUIPermissionDenied;
      case AVAuthorizationStatusRestricted:
        return GUIPermissionRestricted;
    }
  }
  return GUIPermissionUnavailable;
}

int gui_permission_accessibility_status(void) {
  return AXIsProcessTrusted() ? GUIPermissionAuthorized : GUIPermissionNotDetermined;
}

int gui_permission_screen_recording_status(void) {
  return CGPreflightScreenCaptureAccess() ? GUIPermissionAuthorized : GUIPermissionNotDetermined;
}

int gui_permission_automation_status(void) {
  @autoreleasepool {
    // askUserIfNeeded 必须为 false：只读 TCC，禁止弹出授权框。
    NSAppleEventDescriptor *target =
        [NSAppleEventDescriptor descriptorWithBundleIdentifier:@"com.apple.finder"];
    if (target == nil) {
      return GUIPermissionUnavailable;
    }
    OSStatus status = AEDeterminePermissionToAutomateTarget(target.aeDesc, typeWildCard, typeWildCard, false);
    switch (status) {
      case noErr:
        return GUIPermissionAuthorized;
      case errAEEventNotPermitted:
        return GUIPermissionDenied;
      case errAEEventWouldRequireUserConsent:
        return GUIPermissionNotDetermined;
      default:
        return GUIPermissionUnavailable;
    }
  }
}

int gui_permission_notifications_status(void) {
  @autoreleasepool {
    // UserNotifications 在无应用 bundle 的测试进程中不稳定，明确返回不可用。
    NSString *bundleIdentifier = NSBundle.mainBundle.bundleIdentifier;
    if (bundleIdentifier == nil || bundleIdentifier.length == 0) {
      return GUIPermissionUnavailable;
    }
    dispatch_semaphore_t semaphore = dispatch_semaphore_create(0);
    __block int result = GUIPermissionUnavailable;
    [UNUserNotificationCenter.currentNotificationCenter
        getNotificationSettingsWithCompletionHandler:^(UNNotificationSettings *settings) {
          switch (settings.authorizationStatus) {
            case UNAuthorizationStatusAuthorized:
            case UNAuthorizationStatusProvisional:
              result = GUIPermissionAuthorized;
              break;
            case UNAuthorizationStatusNotDetermined:
              result = GUIPermissionNotDetermined;
              break;
            case UNAuthorizationStatusDenied:
              result = GUIPermissionDenied;
              break;
            default:
              result = GUIPermissionUnavailable;
              break;
          }
          dispatch_semaphore_signal(semaphore);
        }];
    dispatch_time_t timeout = dispatch_time(DISPATCH_TIME_NOW, (int64_t)(1500 * NSEC_PER_MSEC));
    dispatch_semaphore_wait(semaphore, timeout);
    return result;
  }
}
