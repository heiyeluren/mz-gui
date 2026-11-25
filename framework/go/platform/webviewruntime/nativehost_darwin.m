#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>

char *mzgui_webkit_version(void) {
	@autoreleasepool {
		NSBundle *bundle = [NSBundle bundleWithIdentifier:@"com.apple.WebKit"];
		if (bundle == nil) {
			bundle = [NSBundle bundleWithPath:@"/System/Library/Frameworks/WebKit.framework"];
		}
		if (bundle == nil) {
			return NULL;
		}
		if (![bundle isLoaded] && ![bundle load]) {
			return NULL;
		}
		if (NSClassFromString(@"WKWebView") == nil) {
			return NULL;
		}
		NSString *version = [bundle objectForInfoDictionaryKey:@"CFBundleVersion"];
		if (version.length == 0) {
			version = [bundle objectForInfoDictionaryKey:@"CFBundleShortVersionString"];
		}
		if (version.length == 0) {
			return NULL;
		}
		return strdup(version.UTF8String);
	}
}
