"""显式准备指定短视频的抽帧资料；不调用模型，不读取浏览器凭证。"""
import json, pathlib, subprocess, time, sys, argparse
import imageio_ffmpeg
import yt_dlp
from video_sampling import extract_visual_frames, VISUAL_INTERVAL_SECONDS
root=pathlib.Path(__file__).resolve().parents[1]
parser=argparse.ArgumentParser();parser.add_argument('--url',required=True);parser.add_argument('--output',required=True);parser.add_argument('--max-minutes',type=int,default=10);args=parser.parse_args()
if not 1<=args.max_minutes<=180:raise SystemExit('max-minutes应为1–180')
out=pathlib.Path(args.output).resolve()
if (out/'manifest.json').exists() or (out/'result-initial.json').exists():raise SystemExit('实验目录已有资料或回执，请使用新目录，避免覆盖')
out.mkdir(parents=True,exist_ok=True)
started=time.perf_counter();stages={}
t=time.perf_counter()
with yt_dlp.YoutubeDL({'quiet':True,'no_warnings':True,'noplaylist':True,'socket_timeout':20,'retries':1,'max_filesize':200*1024*1024,'format':'bestvideo[height<=720][vcodec^=avc1]/best[height<=720]','outtmpl':str(out/'source.%(ext)s')}) as y:
 info=y.extract_info(args.url,download=False)
 if not 0<float(info.get('duration',0))<=args.max_minutes*60:raise SystemExit('时长未知或超过配置上限，停止')
 stages['metadataMs']=round((time.perf_counter()-t)*1000)
 t=time.perf_counter();y.process_info(info);stages['downloadMs']=round((time.perf_counter()-t)*1000)
 source=pathlib.Path(y.prepare_filename(info))
ff=imageio_ffmpeg.get_ffmpeg_exe()
t=time.perf_counter()
frames=extract_visual_frames(source,out,float(info['duration']),ff)
stages['initialFramesMs']=round((time.perf_counter()-t)*1000)
manifest={'sourceId':info['id'],'platform':info.get('extractor_key','unknown'),'sourceUrl':args.url,'title':info['title'],'durationSeconds':info['duration'],'description':info.get('description',''),'subtitleTracks':list(info.get('subtitles',{})),'source':str(source),'frames':frames,'visualSampling':{'intervalSeconds':VISUAL_INTERVAL_SECONDS,'framesPerMinute':3,'strategy':'bucket-midpoint','frameCount':len(frames)},'stages':stages,'prepareTotalMs':round((time.perf_counter()-started)*1000)}
(out/'manifest.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2))
print(json.dumps({k:v for k,v in manifest.items() if k not in ['source','frames']},ensure_ascii=False))
