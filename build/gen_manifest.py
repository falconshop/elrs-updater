"""After build_all.py, copy the built bins into the updater's firmware/ folder and
generate manifest.json. Only variants whose bin actually exists are included.

Run:  python gen_manifest.py
"""
import os, json, shutil, gzip

BINS = r"C:/ClaudeELRS/build/bins"
FW   = r"C:/ClaudeELRS/firmware"
os.makedirs(FW, exist_ok=True)

# (out_name, display_name, match_substring)  -- order: specific matches first
MODELS = [
    ("BETAFPV_Nano",      "BetaFPV Nano 2.4GHz",        "BETAFPV 2.4GHz Nano"),
    ("BETAFPV_Lite",      "BetaFPV Lite 2.4GHz",        "BETAFPV 2.4GHz Lite"),
    ("BETAFPV_SuperD",    "BetaFPV SuperD 2.4GHz",      "BETAFPV SuperD"),
    ("RadioMaster_RP1",   "RadioMaster RP1 2.4GHz",     "RadioMaster RP1"),
    ("RadioMaster_RP2",   "RadioMaster RP2 2.4GHz",     "RadioMaster RP2"),
    ("RadioMaster_RP3",   "RadioMaster RP3 2.4GHz",     "RadioMaster RP3"),
    ("RadioMaster_RP4TDM","RadioMaster RP4TD-M 2.4GHz", "RP4TD-M"),
    ("RadioMaster_RP4TD", "RadioMaster RP4TD 2.4GHz",   "RP4TD True"),
    ("RadioMaster_ER4",   "RadioMaster ER4 2.4GHz",     "RadioMaster ER4"),
    ("RadioMaster_ER3Ci", "RadioMaster ER3C-i 2.4GHz",  "ER3C-i"),
    ("RadioMaster_ER5Ci", "RadioMaster ER5C-i 2.4GHz",  "ER5C-i"),
    ("RadioMaster_ER5C_V2","RadioMaster ER5C V2 2.4GHz","ER5A/C V2"),
    ("RadioMaster_ER6GV", "RadioMaster ER6-GV 2.4GHz",  "ER6-GV"),
    ("RadioMaster_ER6",   "RadioMaster ER6 2.4GHz",     "ER6 2.4GHz"),
    ("RadioMaster_ER8GV", "RadioMaster ER8-GV 2.4GHz",  "ER8-GV"),
    ("RadioMaster_ER8",   "RadioMaster ER8 2.4GHz",     "ER8 2.4GHz"),
    ("RadioMaster_XR4",   "RadioMaster XR4 Dual-Band",  "RadioMaster XR4"),
    ("RadioMaster_XR3",   "RadioMaster XR3 Dual-Band",  "RadioMaster XR3"),
    ("RadioMaster_XR1",   "RadioMaster XR1 Dual-Band",  "RadioMaster XR1"),
    ("HelloRadio_HR7E",   "HelloRadio HR7E 2.4GHz",     "HelloRadio HR7E"),
    ("HelloRadio_HR8E",   "HelloRadio HR8E 2.4GHz",     "HelloRadio HR8E"),
    ("Matek_R24D",        "Matek R24-D 2.4GHz",         "MATEK R24-D"),
    ("BAYCK_5PWM",        "BAYCKRC 5xPWM 2.4GHz",       "5xPWM"),
]

# dual-band (LR1121) models: their Global build carries an FCC_915 900MHz domain
DUALBAND = {"RadioMaster_XR1", "RadioMaster_XR3", "RadioMaster_XR4"}

# Only these top-selling models are BUNDLED into the exe; every other model's
# firmware downloads on demand from the GitHub release and is cached.
BUNDLE = {"RadioMaster_RP1", "RadioMaster_ER6", "RadioMaster_RP4TDM",
          "RadioMaster_ER8", "RadioMaster_RP4TD"}

# ESP8285 (ESP8266-family) receivers can't OTA the raw .bin (not enough flash);
# they need the gzip-compressed image, which eboot decompresses on boot. ESP32
# receivers use the raw .bin. (The firmware auto-detects gz by the 0x1F magic and
# skips the target-name check.)
ESP8285 = {"BETAFPV_Nano", "BETAFPV_Lite", "RadioMaster_RP1", "RadioMaster_RP2",
           "RadioMaster_RP3", "RadioMaster_ER4", "RadioMaster_ER3Ci",
           "RadioMaster_ER5Ci", "RadioMaster_ER5C_V2", "Matek_R24D", "BAYCK_5PWM"}

def main():
    # start clean: only bundle files should end up embedded in firmware/
    for f in os.listdir(FW):
        if f.endswith(".bin") or f.endswith(".bin.gz"):
            os.remove(os.path.join(FW, f))
    models_out = []
    n_bins = 0
    for out, disp, match in MODELS:
        variants = []
        gz = out in ESP8285
        for dom in ("KC", "Global"):
            label = "KC펌" if dom == "KC" else ("글로벌(FCC)" if out in DUALBAND else "글로벌펌")
            raw = os.path.join(BINS, f"{out}_{dom}.bin")
            if not (os.path.exists(raw) and os.path.getsize(raw) > 100000):
                continue
            if gz:                                       # ESP8285 -> gzip
                dst_name = f"{out}_{dom}.bin.gz"
                srcfile = os.path.join(BINS, dst_name)
                with open(raw, "rb") as fi, gzip.open(srcfile, "wb") as fo:
                    shutil.copyfileobj(fi, fo)
            else:                                        # ESP32 -> raw
                dst_name = f"{out}_{dom}.bin"
                srcfile = raw
            if out in BUNDLE:                            # embed only bundled models
                shutil.copy(srcfile, os.path.join(FW, dst_name))
                n_bins += 1
            variants.append({"id": dom.lower(), "label": label, "file": dst_name})
        if variants:
            models_out.append({"match": match, "name": disp, "variants": variants})
    manifest = {
        "_comment": "자동 생성. product_name 에 match 포함 시 해당 모델. 번들 5개만 exe 내장, 나머지는 GitHub 릴리스에서 다운로드.",
        "models": models_out,
    }
    with open(os.path.join(FW, "manifest.json"), "w", encoding="utf-8") as f:
        json.dump(manifest, f, ensure_ascii=False, indent=2)
    print(f"copied {n_bins} bins into {FW}")
    print(f"manifest: {len(models_out)} models")
    for m in models_out:
        print("  ", m["name"], "->", [v["id"] for v in m["variants"]])

if __name__ == "__main__":
    main()
