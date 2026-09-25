import glob, json, os, subprocess, numpy as np
out = {}
for p in sorted(glob.glob("/work/vo/final/*.wav")):
    raw = subprocess.run(["ffmpeg","-v","error","-i",p,"-ac","1","-ar","16000","-f","s16le","-"],capture_output=True,check=True).stdout
    x = np.frombuffer(raw, dtype=np.int16).astype(np.float32)/32768
    hop = 160; e = np.array([np.sqrt((x[i:i+320]**2).mean()) for i in range(0, len(x)-320, hop)])
    on = e > max(0.02, e.max()*0.08)
    segs=[]; start=None; gap=0
    for i,v in enumerate(on):
        if v:
            if start is None: start=i
            gap=0; last=i
        elif start is not None:
            gap+=1
            if gap>=12: segs.append((round(start*0.01,2), round(last*0.01,2))); start=None
    if start is not None: segs.append((round(start*0.01,2), round(last*0.01,2)))
    k=os.path.basename(p)[:-4]; out[k]=segs; print(k, segs)
json.dump(out, open("/work/vo/final/segments.json","w"))
