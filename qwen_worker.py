"""Persistent Qwen CustomVoice worker. Stdout is reserved for JSON messages."""
import base64
import json
import sys

protocol = sys.stdout
sys.stdout = sys.stderr


def main():
    import numpy as np
    import torch
    from qwen_tts import Qwen3TTSModel

    device = sys.argv[2]
    if device == "auto":
        device = "cuda:0" if torch.cuda.is_available() else "cpu"
    if device == "cpu":
        torch.set_num_threads(4)
    # Keep existing SID 4 on a native English voice. IDs are stable across runs.
    speakers = ["Ryan", "Vivian", "Serena", "Uncle_Fu", "Aiden",
                "Dylan", "Eric", "Ono_Anna", "Sohee"]
    model = None
    for line in sys.stdin:
        try:
            request = json.loads(line)
            if model is None:
                model = Qwen3TTSModel.from_pretrained(
                    sys.argv[1], device_map=device,
                    dtype=torch.bfloat16 if device.startswith("cuda") else torch.float32,
                    attn_implementation="sdpa",
                )
            speed = float(request["speed"])
            instruction = "Speak naturally and clearly in English."
            if abs(speed - 1.0) > 0.01:
                instruction += f" Speak at {speed:g} times the normal speaking rate."
            with torch.inference_mode():
                wavs, rate = model.generate_custom_voice(
                    text=request["text"], language="English",
                    speaker=speakers[int(request["sid"])], instruct=instruction,
                    max_new_tokens=4096,
                )
            pcm = np.asarray(wavs[0], dtype="<f4").reshape(-1)
            response = {"sample_rate": int(rate),
                        "audio": base64.b64encode(pcm.tobytes()).decode("ascii")}
        except Exception as exc:
            import traceback
            traceback.print_exc(file=sys.stderr)
            response = {"error": str(exc)}
        protocol.write(json.dumps(response) + "\n")
        protocol.flush()


if __name__ == "__main__":
    main()
