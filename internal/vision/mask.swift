// imgkit's Vision helper: `imgkit-vision <in.png> <out.png>` writes the
// image with every foreground instance kept and the rest transparent, at the
// input's size. imgkit reads its alpha as the coarse mask. The mask is
// segmentation only: about 96% of it is hard 0 or 255 with a 2 px
// transition and no hair, so it is a prior for ViTMatte, never a result.

import AppKit
import Vision

func die(_ message: String) -> Never {
  FileHandle.standardError.write("imgkit-vision: \(message)\n".data(using: .utf8)!)
  exit(1)
}

let args = CommandLine.arguments
guard args.count == 3 else { die("usage: imgkit-vision <in.png> <out.png>") }
let handler = VNImageRequestHandler(url: URL(fileURLWithPath: args[1]), options: [:])
let request = VNGenerateForegroundInstanceMaskRequest()
do { try handler.perform([request]) } catch { die("Vision request failed: \(error.localizedDescription)") }
guard let observation = request.results?.first else { die("no foreground found") }
// Every instance, uncropped: a separately segmented arm or strand of hair
// must not be lost, and the caller's coordinates stay the source frame's.
let masked: CVPixelBuffer
do {
  masked = try observation.generateMaskedImage(
    ofInstances: observation.allInstances, from: handler, croppedToInstancesExtent: false)
} catch { die("could not generate the masked image: \(error.localizedDescription)") }
let image = CIImage(cvPixelBuffer: masked)
guard let space = CGColorSpace(name: CGColorSpace.sRGB),
  let png = CIContext().pngRepresentation(of: image, format: .RGBA8, colorSpace: space)
else { die("could not encode PNG") }
do { try png.write(to: URL(fileURLWithPath: args[2])) } catch { die("could not write \(args[2])") }
