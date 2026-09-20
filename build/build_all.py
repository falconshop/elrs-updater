"""Batch-build all shop-receiver firmware bins: KC (fork) + Global (official 3.6.4).

Uses the ELRS_BOARD_CONFIG env-var patch so board_config changes DON'T force a
recompile (only the first build of each env compiles; the rest are fast re-bakes).
Bins land in build/bins/<out>_<KC|Global>.bin, grouped by env to keep the cache warm.
"""
import os, subprocess, shutil, sys, time, re

KC_SRC   = r"C:/ELRS/ExpressLRS-KC364/src"
GLOB_SRC = r"C:/ELRS/ExpressLRS-official/src"
OUT      = r"C:/ClaudeELRS/build/bins"
os.makedirs(OUT, exist_ok=True)

# (out_name, board_config, env, build_KC, build_Global, expect_substr_in_bin)
E8285 = "Unified_ESP8285_2400_RX_via_UART"
E32   = "Unified_ESP32_2400_RX_via_UART"
ELR   = "Unified_ESP32_LR1121_RX_via_UART"
EC3   = "Unified_ESP32C3_LR1121_RX_via_UART"

MODELS = [
    # ESP8285 2.4
    ("BETAFPV_Nano",   "betafpv.rx_2400.nano",       E8285, True,  True,  "Nano"),
    ("BETAFPV_Lite",   "betafpv.rx_2400.lite",       E8285, True,  True,  "Lite"),
    ("RadioMaster_RP1","radiomaster.rx_2400.rp1",    E8285, True,  True,  "RP1"),
    ("RadioMaster_RP2","radiomaster.rx_2400.rp2",    E8285, True,  True,  "RP2"),
    ("RadioMaster_RP3","radiomaster.rx_2400.rp3",    E8285, True,  True,  "RP3"),
    ("RadioMaster_ER4","radiomaster.rx_2400.er4",    E8285, True,  True,  "ER4"),
    ("RadioMaster_ER3Ci","radiomaster.rx_2400.er3",  E8285, True,  True,  "ER3C-i"),
    ("RadioMaster_ER5Ci","radiomaster.rx_2400.er5c-i",E8285,True,  True,  "ER5C-i"),
    ("RadioMaster_ER5C_V2","radiomaster.rx_2400.er5-v2",E8285,True,True,  "V2"),
    ("Matek_R24D",     "matek.rx_2400.r24d",         E8285, True,  True,  "R24-D"),
    ("BAYCK_5PWM",     "generic.rx_2400.pwm5",       E8285, True,  True,  "5xPWM"),
    # ESP32 2.4
    ("BETAFPV_SuperD", "betafpv.rx_2400.superd",     E32,   True,  True,  "SuperD"),
    ("RadioMaster_RP4TD","radiomaster.rx_2400.rp4",  E32,   True,  True,  "RP4TD True"),
    ("RadioMaster_RP4TDM","radiomaster.rx_2400.rp4m",E32,   True,  True,  "RP4TD-M"),
    ("RadioMaster_ER6","radiomaster.rx_2400.er6",    E32,   True,  True,  "ER6 2.4"),
    ("RadioMaster_ER6GV","radiomaster.rx_2400.er6gv",E32,   True,  True,  "ER6-GV"),
    ("RadioMaster_ER8","radiomaster.rx_2400.er8",    E32,   True,  True,  "ER8 2.4"),
    ("RadioMaster_ER8GV","radiomaster.rx_2400.er8gv",E32,   True,  True,  "ER8-GV"),
    ("HelloRadio_HR7E","helloradio.rx_2400.hr7e",    E32,   True,  True,  "HR7E"),
    ("HelloRadio_HR8E","helloradio.rx_2400.hr8e",    E32,   True,  True,  "HR8E"),
    # ESP32 LR1121 (dual band, non-C3) -> KC ok
    ("RadioMaster_XR4","radiomaster.rx_dual.xr4",    ELR,   True,  True,  "XR4"),
    # ESP32-C3 LR1121 -> Global only (fork has no C3 env)
    ("RadioMaster_XR3","radiomaster.rx_dual.xr3",    EC3,   True , True,  "XR3"),
    ("RadioMaster_XR1","radiomaster.rx_dual.xr1",    EC3,   True , True,  "XR1"),
]

# order by env so the compile cache stays warm within each env
ENV_ORDER = [E8285, E32, ELR, EC3]
MODELS.sort(key=lambda m: ENV_ORDER.index(m[2]))

def build(src, model, domain):
    out_name, cfg, env, kc, gl, expect = model
    dst = os.path.join(OUT, f"{out_name}_{domain}.bin")
    if os.path.exists(dst) and os.path.getsize(dst) > 100000:
        return "skip(exists)"
    envv = dict(os.environ)
    envv["ELRS_BOARD_CONFIG"] = cfg
    envv["PYTHONPATH"] = os.path.join(src, "python", "external", "esptool")
    t = time.time()
    r = subprocess.run(["pio", "run", "-e", env], cwd=src, env=envv,
                       stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    dt = int(time.time() - t)
    if r.returncode != 0:
        tail = "\n".join(r.stdout.splitlines()[-6:])
        return f"FAIL({dt}s)\n{tail}"
    binpath = os.path.join(src, ".pio", "build", env, "firmware.bin")
    if not os.path.exists(binpath):
        return f"FAIL({dt}s) no firmware.bin"
    data = open(binpath, "rb").read()
    ok = expect.encode() in data
    shutil.copy(binpath, dst)
    return f"ok({dt}s){'' if ok else ' [WARN expect not found]'}"

def main():
    plan = []
    for m in MODELS:
        if m[3]: plan.append((KC_SRC, m, "KC"))
    for m in MODELS:
        if m[4]: plan.append((GLOB_SRC, m, "Global"))
    total = len(plan)
    print(f"=== building {total} bins -> {OUT} ===", flush=True)
    fails = []
    for i,(src,m,dom) in enumerate(plan,1):
        res = build(src, m, dom)
        line = f"[{i:2}/{total}] {dom:6} {m[0]:22} {res.splitlines()[0]}"
        print(line, flush=True)
        if res.startswith("FAIL"):
            fails.append((dom,m[0],res))
            print(res, flush=True)
    print("\n=== DONE ===", flush=True)
    print(f"built: {total-len(fails)}/{total}", flush=True)
    if fails:
        print("FAILURES:", flush=True)
        for d,n,_ in fails: print(f"  {d} {n}", flush=True)
    # list output
    bins = sorted(os.listdir(OUT))
    print(f"\n{len(bins)} bin files:", flush=True)
    for b in bins: print("  ", b, os.path.getsize(os.path.join(OUT,b)), flush=True)

if __name__ == "__main__":
    main()
