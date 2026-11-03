"""Visual sampling independent of OCR; no network, model or platform calls."""
import math

VISUAL_INTERVAL_SECONDS = 20
MAX_DURATION_SECONDS = 180 * 60


def visual_sample_seconds(duration):
    """One midpoint per 20-second bucket, including a shorter final bucket."""
    if isinstance(duration, bool) or not isinstance(duration, (int, float)):
        raise ValueError("duration must be a number")
    if not math.isfinite(duration) or not 0 < duration <= MAX_DURATION_SECONDS:
        raise ValueError("duration must be finite, positive and at most 180 minutes")
    return [
        (start + min(start + VISUAL_INTERVAL_SECONDS, duration)) / 2
        for start in range(0, math.ceil(duration), VISUAL_INTERVAL_SECONDS)
    ]


def extract_visual_frames(source, output, duration, ffmpeg):
    """Extract each requested frame or fail; never report a missing frame as read."""
    import pathlib
    import subprocess

    output = pathlib.Path(output)
    output.mkdir(parents=True, exist_ok=True)
    frames = []
    for index, seconds in enumerate(visual_sample_seconds(duration), start=1):
        path = output / f"frame-{index:03d}.jpg"
        subprocess.run([
            ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error",
            "-ss", str(seconds), "-i", str(source), "-frames:v", "1",
            "-vf", "scale=640:640:force_original_aspect_ratio=decrease",
            "-q:v", "3", "-y", str(path),
        ], check=True, timeout=30)
        if not path.is_file() or path.stat().st_size == 0:
            raise RuntimeError(f"frame missing at {seconds}s")
        frames.append({"seconds": seconds, "path": str(path.resolve())})
    return frames
