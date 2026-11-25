#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>

static int mzfw_read_clipboard_file_paths(char **output, size_t *length) {
  @autoreleasepool {
    NSMutableArray<NSString *> *paths = [NSMutableArray array];
    NSMutableSet<NSString *> *seen = [NSMutableSet set];

    void (^appendURL)(NSURL *) = ^(NSURL *url) {
      if (url == nil || !url.isFileURL) {
        return;
      }
      NSString *path = url.URLByStandardizingPath.path;
      if (path.length == 0 || [seen containsObject:path]) {
        return;
      }
      [seen addObject:path];
      [paths addObject:path];
    };

    NSPasteboard *pasteboard = NSPasteboard.generalPasteboard;
    NSArray *objects = [pasteboard readObjectsForClasses:@[NSURL.class]
                                                 options:@{NSPasteboardURLReadingFileURLsOnlyKey: @YES}] ?: @[];
    for (id object in objects) {
      if ([object isKindOfClass:NSURL.class]) {
        appendURL((NSURL *)object);
      }
    }
    for (NSPasteboardItem *item in pasteboard.pasteboardItems ?: @[]) {
      NSString *value = [item stringForType:NSPasteboardTypeFileURL];
      if (value.length > 0) {
        appendURL([NSURL URLWithString:value]);
      }
    }

    NSError *error = nil;
    NSData *data = [NSJSONSerialization dataWithJSONObject:paths options:0 error:&error];
    if (error != nil || data == nil) {
      return 1;
    }
    char *buffer = malloc(data.length);
    if (buffer == NULL && data.length > 0) {
      return 1;
    }
    if (data.length > 0) {
      memcpy(buffer, data.bytes, data.length);
    }
    *output = buffer;
    *length = data.length;
    return 0;
  }
}

int mzfw_clipboard_file_paths_json(char **output, size_t *length) {
  if (output == NULL || length == NULL) {
    return 1;
  }
  *output = NULL;
  *length = 0;
  if (NSThread.isMainThread) {
    return mzfw_read_clipboard_file_paths(output, length);
  }
  __block int result = 1;
  dispatch_sync(dispatch_get_main_queue(), ^{
    result = mzfw_read_clipboard_file_paths(output, length);
  });
  return result;
}

static int mzfw_read_clipboard_image_png(char **output, size_t *length, size_t max_raw_bytes) {
  @autoreleasepool {
    NSPasteboard *pasteboard = NSPasteboard.generalPasteboard;
    NSData *data = [pasteboard dataForType:NSPasteboardTypePNG];
    if (data == nil) {
      NSData *tiff = [pasteboard dataForType:NSPasteboardTypeTIFF];
      if (tiff == nil) {
        return 2;
      }
      if (tiff.length > max_raw_bytes) {
        return 3;
      }
      NSBitmapImageRep *representation = [NSBitmapImageRep imageRepWithData:tiff];
      if (representation == nil || representation.pixelsWide < 1 || representation.pixelsHigh < 1) {
        return 1;
      }
      if ((uint64_t)representation.pixelsWide * (uint64_t)representation.pixelsHigh > 40000000ULL) {
        return 3;
      }
      data = [representation representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
    }
    if (data == nil) {
      return 1;
    }
    if (data.length > max_raw_bytes) {
      return 3;
    }
    char *buffer = malloc(data.length);
    if (buffer == NULL && data.length > 0) {
      return 1;
    }
    if (data.length > 0) {
      memcpy(buffer, data.bytes, data.length);
    }
    *output = buffer;
    *length = data.length;
    return 0;
  }
}

int mzfw_clipboard_image_png(char **output, size_t *length, size_t max_raw_bytes) {
  if (output == NULL || length == NULL || max_raw_bytes == 0) {
    return 1;
  }
  *output = NULL;
  *length = 0;
  if (NSThread.isMainThread) {
    return mzfw_read_clipboard_image_png(output, length, max_raw_bytes);
  }
  __block int result = 1;
  dispatch_sync(dispatch_get_main_queue(), ^{
    result = mzfw_read_clipboard_image_png(output, length, max_raw_bytes);
  });
  return result;
}
