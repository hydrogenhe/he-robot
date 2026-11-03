"""Verify sampling boundaries and the real analysis entry without model calls."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SCRIPTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SCRIPTS))
from video_sampling import visual_sample_seconds


class VideoSamplingTest(unittest.TestCase):
    def test_duration_boundaries_and_midpoints(self):
        cases = [
            (1, [0.5]), (20, [10]), (21, [10, 20.5]),
            (60, [10, 30, 50]), (61, [10, 30, 50, 60.5]),
            (277, list(range(10, 251, 20)) + [268.5]),
        ]
        for duration, expected in cases:
            with self.subTest(duration=duration):
                self.assertEqual(visual_sample_seconds(duration), expected)
        self.assertEqual(len(visual_sample_seconds(600)), 30)
        self.assertEqual(len(visual_sample_seconds(10800)), 540)
        for duration in (0.001, 19.999, 20.001, 59.999):
            self.assertTrue(all(0 <= point < duration for point in visual_sample_seconds(duration)))

    def test_invalid_durations(self):
        for duration in (0, -1, float('nan'), float('inf'), True, '60', None, 10801):
            with self.subTest(duration=duration):
                with self.assertRaises(ValueError):
                    visual_sample_seconds(duration)

    def test_analysis_sends_actual_frame_count_once(self):
        with tempfile.TemporaryDirectory() as name:
            directory = Path(name)
            frames = [{'seconds': value, 'path': str(directory / f'{index}.jpg')}
                      for index, value in enumerate(visual_sample_seconds(277))]
            manifest = {'title': '虚构测试视频', 'durationSeconds': 277, 'frames': frames,
                        'stages': {}, 'visualSampling': {'intervalSeconds': 20, 'frameCount': 14}}
            (directory / 'manifest.json').write_text(json.dumps(manifest))
            (directory / 'subtitles-ocr.json').write_text('[]')
            fake = directory / 'model-fixture'
            fake.write_text('''#!/usr/bin/env python3
import json, sys
from pathlib import Path
args = sys.argv
request = json.loads(Path(args[args.index('-request')+1]).read_text())
assert len(request['images']) == 14
assert len(request['body']['frameSeconds']) == 14
assert '14张' in request['instruction']
assert '仅14张' in request['body']['evidence']
result = {'result': {'reply': {'Text': json.dumps({'sufficient': True, 'requestSeconds': []}), 'Usage': {'input': 1, 'output': 1}}}}
Path(args[args.index('-output')+1]).write_text(json.dumps(result))
''')
            fake.chmod(0o700)
            command = [sys.executable, str(SCRIPTS / 'video_experiment_analyze.py'),
                       '--input', str(directory), '--model-bin', str(fake)]
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            saved = json.loads((directory / 'manifest.json').read_text())
            self.assertEqual(saved['modelInput']['images'], 14)
            self.assertEqual(saved['supplementImages'], 0)
            self.assertFalse((directory / 'result-supplement.json').exists())
            # A completed receipt blocks a second invocation before reaching the fixture.
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('已有真实回执', result.stderr)


if __name__ == '__main__':
    unittest.main()
