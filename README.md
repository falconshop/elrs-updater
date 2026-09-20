# ELRS 수신기 펌웨어 업데이터 (팰콘샵)

손님이 **선/어댑터/파이썬 없이** ExpressLRS 수신기 펌웨어를 스스로 업데이트할 수 있는 단일 exe 도구.
수신기 내장 WiFi(OTA)를 이용하므로 **베타플라이트·아두파일럿·PX4·FC 없이 서보만** 쓰는 경우 모두 동일하게 동작한다.

## 손님 사용법 (아주 간단)

1. 수신기에 **전원**을 넣는다 (조종기는 **꺼둔 채로**).
2. 약 **60초** 기다리면 수신기가 WiFi를 켠다.
3. PC를 WiFi **`ExpressLRS RX`** 에 연결한다 (암호: `expresslrs`).
4. **`ELRS-Updater.exe`** 실행 → 창이 뜨고 수신기를 자동 인식 → **업데이트 시작** 클릭.
5. 완료 메시지가 뜰 때까지, 그리고 LED가 다시 깜빡일 때까지 **전원을 빼지 않는다**.

> 폰만 있어도 가능: `ExpressLRS RX` WiFi에 접속 후 브라우저로 `http://10.0.0.1` 접속 → 펌 파일 업로드.
> exe는 이 과정을 자동화하고 올바른 펌을 자동 선택해 줄 뿐이다.

## 사장님: 펌웨어 넣고 빌드하기

1. 모델별 펌웨어 `.bin`을 `firmware\` 폴더에 넣는다.
   - 이 bin은 ELRS 소스에서 만든 것 (기기선택 프롬프트에서 해당 모델을 구운 최종 `firmware.bin`).
2. `firmware\manifest.json` 에 등록한다:
   ```json
   { "match": "HelloRadio HR8E", "file": "HelloRadio_HR8E.bin", "label": "HelloRadio HR8E 2.4GHz" }
   ```
   - `match`: 수신기의 `GET /target` product_name 에 포함되는 문자열 (대소문자 무시).
   - `file`: `firmware\` 안의 파일명.
3. **`build.bat`** 더블클릭(또는 명령창에서 실행) → `ELRS-Updater.exe` 생성.
4. 여러 모델의 bin을 넣어두면 **연결된 수신기를 자동 감지**해 맞는 펌을 굽는다(통합 exe).

## 동작 원리 (요약)

- `GET  http://10.0.0.1/target` → 연결된 수신기 모델/버전 파악·표시.
- `POST http://10.0.0.1/update` (multipart `data`=bin, 헤더 `X-FileSize`) → 펌 업로드.
- 수신기 펌웨어가 **모델 불일치 bin은 자체 거부**(`status: mismatch`)하므로 오배포로 벽돌 위험이 낮다.

## 개발/테스트

- 실기 테스트(콘솔): `elrs-updater-cli.exe <firmware.bin>` — 실제 수신기를 WiFi로 연결한 상태에서 외부 bin을 굽는다.
- 하드웨어 없이 UI 테스트: `test\mock_rx.py` 로 가짜 수신기를 띄우고
  `set ELRS_HOST=127.0.0.1:8899` `set ELRS_ADDR=127.0.0.1:8080` `set ELRS_NOBROWSER=1` 로 실행 후 브라우저로 확인.

## 환경변수(테스트용)

- `ELRS_HOST` : 수신기 주소 (기본 `10.0.0.1`)
- `ELRS_ADDR` : 로컬 UI 서버 바인드 주소 (기본 랜덤 포트)
- `ELRS_NOBROWSER` : 설정 시 Edge 앱 창을 열지 않고 자동종료도 끔(테스트)
