#import <Foundation/Foundation.h>

int mzfw_trash_directory(const char *path) {
  @autoreleasepool {
    NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path] isDirectory:YES];
    NSError *error = nil;
    NSURL *result = nil;
    return [[NSFileManager defaultManager] trashItemAtURL:url resultingItemURL:&result error:&error] ? 0 : 1;
  }
}
