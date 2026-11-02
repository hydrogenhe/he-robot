"""为当前实验提取每秒字幕画面、本地OCR和相邻去重；原始识别结果保留。"""
import json,pathlib,subprocess,time,argparse,platform
import imageio_ffmpeg
parser=argparse.ArgumentParser();parser.add_argument('--input',required=True);parser.add_argument('--ocr-bin',required=True);args=parser.parse_args()
if platform.system()!='Darwin':raise SystemExit('当前Apple Vision适配仅支持macOS；请替换OCR引擎，后续JSON协议通用')
out=pathlib.Path(args.input).resolve();manifest=json.loads((out/'manifest.json').read_text());frames=out/'ocr-frames';frames.mkdir(exist_ok=True)
t=time.perf_counter()
subprocess.run([imageio_ffmpeg.get_ffmpeg_exe(),'-nostdin','-hide_banner','-loglevel','error','-i',manifest['source'],'-vf','fps=1:start_time=0','-frames:v','600','-q:v','3','-y',str(frames/'%04d.jpg')],check=True,timeout=90)
manifest['stages']['ocrFramesMs']=round((time.perf_counter()-t)*1000);t=time.perf_counter()
subprocess.run([args.ocr_bin,str(frames),str(out/'ocr-raw.json')],check=True,timeout=240)
manifest['stages']['ocrMs']=round((time.perf_counter()-t)*1000)
rows=json.loads((out/'ocr-raw.json').read_text());lines=[];previous=''
for row in rows:
 text=' '.join(x['text'] for x in row['lines']).strip()
 if text and text!=previous:lines.append({'seconds':row['seconds'],'text':text})
 previous=text
(out/'subtitles-ocr.json').write_text(json.dumps(lines,ensure_ascii=False,indent=2))
(out/'subtitles-ocr.txt').write_text('\n'.join(f"[{x['seconds']}s] {x['text']}" for x in lines))
manifest['ocr']={'frames':len(rows),'uniqueAdjacentLines':len(lines),'characters':sum(len(x['text']) for x in lines),'intervalSeconds':1,'region':'x=.03 y=.12 w=.94 h=.42 (bottom-left origin)','engine':'Apple Vision local','audioRead':False}
(out/'manifest.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2));print(json.dumps({'ocr':manifest['ocr'],'stages':manifest['stages']},ensure_ascii=False))
