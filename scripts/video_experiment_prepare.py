"""显式准备指定短视频的抽帧资料；不调用模型，不读取浏览器凭证。"""
import json, pathlib, subprocess, time, sys, argparse
import imageio_ffmpeg
import yt_dlp
root=pathlib.Path(__file__).resolve().parents[1]
parser=argparse.ArgumentParser();parser.add_argument('--url',required=True);parser.add_argument('--output',required=True);parser.add_argument('--max-minutes',type=int,default=10);args=parser.parse_args()
if not 1<=args.max_minutes<=180:raise SystemExit('max-minutes应为1–180')
out=pathlib.Path(args.output).resolve();out.mkdir(parents=True,exist_ok=True)
started=time.perf_counter();stages={}
t=time.perf_counter()
with yt_dlp.YoutubeDL({'quiet':True,'no_warnings':True,'noplaylist':True,'socket_timeout':20,'retries':1,'max_filesize':200*1024*1024,'format':'bestvideo[height<=720][vcodec^=avc1]/best[height<=720]','outtmpl':str(out/'source.%(ext)s')}) as y:
 info=y.extract_info(args.url,download=False)
 if not 0<float(info.get('duration',0))<=args.max_minutes*60:raise SystemExit('时长未知或超过配置上限，停止')
 stages['metadataMs']=round((time.perf_counter()-t)*1000)
 t=time.perf_counter();y.process_info(info);stages['downloadMs']=round((time.perf_counter()-t)*1000)
 source=pathlib.Path(y.prepare_filename(info))
ff=imageio_ffmpeg.get_ffmpeg_exe()
t=time.perf_counter();frames=[]
for i in range(6):
 sec=round(info['duration']*(i+.5)/6,3);path=out/f'frame-{i+1:02d}.jpg'
 subprocess.run([ff,'-nostdin','-hide_banner','-loglevel','error','-ss',str(sec),'-i',str(source),'-frames:v','1','-vf','scale=640:640:force_original_aspect_ratio=decrease','-q:v','3','-y',str(path)],check=True,timeout=30)
 frames.append({'seconds':sec,'path':str(path)})
stages['initialFramesMs']=round((time.perf_counter()-t)*1000)
manifest={'sourceId':info['id'],'platform':info.get('extractor_key','unknown'),'sourceUrl':args.url,'title':info['title'],'durationSeconds':info['duration'],'description':info.get('description',''),'subtitleTracks':list(info.get('subtitles',{})),'source':str(source),'frames':frames,'stages':stages,'prepareTotalMs':round((time.perf_counter()-started)*1000)}
(out/'manifest.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2))
print(json.dumps({k:v for k,v in manifest.items() if k not in ['source','frames']},ensure_ascii=False))
