// Gives an app the icon of another app with its hue rotated, as a custom
// Finder icon (Icon\r in the bundle), which the Dock and Finder show.
// osascript -l JavaScript tint.js <app> <icon-from-app> <degrees>
ObjC.import('Foundation');
ObjC.import('AppKit');
ObjC.import('CoreImage');

function run(argv) {
  var app = argv[0], from = argv[1], hue = parseFloat(argv[2]);
  var info = $.NSDictionary.dictionaryWithContentsOfFile(from + '/Contents/Info.plist');
  if (info.isNil()) throw new Error('cannot read ' + from + '/Contents/Info.plist');
  var name = info.objectForKey('CFBundleIconFile');
  if (name.isNil()) throw new Error(from + ' names no CFBundleIconFile');
  name = name.js;
  if (name.slice(-5).toLowerCase() !== '.icns') name += '.icns';
  var data = $.NSData.dataWithContentsOfFile(from + '/Contents/Resources/' + name);
  if (data.isNil()) throw new Error('cannot read the icon ' + name);
  var ci = $.CIImage.imageWithData(data);
  if (ci.isNil()) throw new Error('the icon is not an image');
  var f = $.CIFilter.filterWithName('CIHueAdjust');
  f.setDefaults;
  f.setValueForKey(ci, 'inputImage');
  f.setValueForKey($.NSNumber.numberWithDouble(hue * Math.PI / 180), 'inputAngle');
  // Rendered into a bitmap first: NSWorkspace keeps a blank icon otherwise.
  var rep = $.NSCIImageRep.imageRepWithCIImage(f.valueForKey('outputImage'));
  var img = $.NSImage.alloc.initWithSize(rep.size);
  img.addRepresentation(rep);
  var bmp = $.NSBitmapImageRep.imageRepWithData(img.TIFFRepresentation);
  var out = $.NSImage.alloc.initWithSize(bmp.size);
  out.addRepresentation(bmp);
  if (!$.NSWorkspace.sharedWorkspace.setIconForFileOptions(out, app, 0)) throw new Error('could not set the icon of ' + app);
  return 'ok';
}
