// 本地 Apple Vision OCR；只读取指定抽帧目录，不访问网络或模型。
import Foundation
import Vision
import ImageIO
let input = URL(fileURLWithPath: CommandLine.arguments[1])
let output = URL(fileURLWithPath: CommandLine.arguments[2])
let files = try FileManager.default.contentsOfDirectory(at: input, includingPropertiesForKeys: nil).filter{$0.pathExtension == "jpg"}.sorted{$0.lastPathComponent < $1.lastPathComponent}
var rows: [[String: Any]] = []
for (index, file) in files.enumerated() {
 try autoreleasepool {
  let request = VNRecognizeTextRequest()
  request.recognitionLevel = .accurate
  request.recognitionLanguages = ["zh-Hans", "en-US"]
  request.usesLanguageCorrection = false
  // 字幕在画面下部中央；排除顶部水印与最底部固定导航。
  request.regionOfInterest = CGRect(x:0.03,y:0.12,width:0.94,height:0.42)
  let handler = VNImageRequestHandler(url:file,options:[:])
  try handler.perform([request])
  let lines = (request.results ?? []).sorted{$0.boundingBox.midY > $1.boundingBox.midY}.compactMap{$0.topCandidates(1).first}.map{["text":$0.string,"confidence":$0.confidence] as [String:Any]}
  rows.append(["seconds":index,"lines":lines])
 }
}
try JSONSerialization.data(withJSONObject: rows,options:[.prettyPrinted,.sortedKeys]).write(to:output)
print("OCR frames: \(rows.count)")
