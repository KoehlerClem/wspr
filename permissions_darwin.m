// macOS permission helpers: drive the Microphone and Accessibility prompts.
#import <Foundation/Foundation.h>
#import <AVFoundation/AVFoundation.h>
#import <ApplicationServices/ApplicationServices.h>

// requestMicrophone shows the microphone prompt if the choice has not been made
// yet, and blocks until the user answers. AVCaptureDevice's status getter is
// cached, so the completion handler — not polling — is what tells us it is done.
void requestMicrophone(void) {
    if ([AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio]
            != AVAuthorizationStatusNotDetermined) {
        return;
    }
    dispatch_semaphore_t sem = dispatch_semaphore_create(0);
    [AVCaptureDevice requestAccessForMediaType:AVMediaTypeAudio
                             completionHandler:^(BOOL granted) {
        (void)granted;
        dispatch_semaphore_signal(sem);
    }];
    dispatch_semaphore_wait(sem, DISPATCH_TIME_FOREVER);
}

// microphoneStatus reports the current microphone authorization without
// prompting: 0 = not determined, 1 = denied/restricted, 2 = authorized.
int microphoneStatus(void) {
    switch ([AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio]) {
        case AVAuthorizationStatusAuthorized:    return 2;
        case AVAuthorizationStatusNotDetermined: return 0;
        default:                                 return 1;
    }
}

void requestAccessibility(void) {
    NSDictionary *opts = @{(id)kAXTrustedCheckOptionPrompt : @YES};
    (void)AXIsProcessTrustedWithOptions((CFDictionaryRef)opts);
}

int accessibilityTrusted(void) {
    return AXIsProcessTrusted() ? 1 : 0;
}
