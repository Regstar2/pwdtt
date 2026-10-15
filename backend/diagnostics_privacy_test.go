package backend

import (
    "context"
    "strings"
    "testing"
)

func TestIssue25SanitizeSensitiveFormats(t *testing.T) {
    cases := []struct{
        name string
        input string
        secret string
        marker string
    }{
        {"vk raw link", "VK call https://vk.com/call/join/SecretJoinHash", "SecretJoinHash", "[link redacted]"},
        {"vk bare link", "vk.com/call/join/SecretJoinHash", "SecretJoinHash", "[link redacted]"},
        {"vk percent encoded link", "link=https%3A%2F%2Fvk.com%2Fcall%2Fjoin%2FSecretJoinHash&anonymName=Secret+Person", "SecretJoinHash", "[link redacted]"},
        {"vk double encoded link", "link%3Dhttps%253A%252F%252Fvk.com%252Fcall%252Fjoin%252FSecretJoinHash", "SecretJoinHash", "[link redacted]"},
        {"raw anonymous name", "anonymName=SecretPerson&worker=1", "SecretPerson", "[redacted]"},
        {"encoded name field", "anonymName%3DSecretPerson", "SecretPerson", "[redacted]"},
        {"json anonymous name", `{"anonymName":"Secret Person","ok":true}`, "Secret Person", "[redacted]"},
        {"json username", `{"username":"Secret Person"}`, "Secret Person", "[redacted]"},
        {"json id", `{"user_id":987654321}`, "987654321", "[redacted]"},
        {"device id", "device_id=SecretDeviceID", "SecretDeviceID", "[redacted]"},
        {"call hash", "call_hash=SecretCallHash", "SecretCallHash", "[redacted]"},
        {"profile alias", "server=SecretAlias", "SecretAlias", "[redacted]"},
        {"query parameter", "?unknown_parameter=SecretValue", "SecretValue", "[redacted]"},
        {"windows path", `config C:\Users\SecretUser\AppData\Roaming\pwdtt`, "SecretUser", "[path redacted]"},
        {"windows forward path", "C:/Users/SecretUser/AppData/Roaming/pwdtt", "SecretUser", "[path redacted]"},
        {"unix user path", "/home/SecretUser/.config/pwdtt", "SecretUser", "[path redacted]"},
        {"mac user path", "/Users/SecretUser/Library/Application Support", "SecretUser", "[path redacted]"},
        {"unc path", `\\HOST\share\SecretUser\file`, "SecretUser", "[path redacted]"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T){
            result := sanitizeDiagnosticText(tc.input)
            if strings.Contains(result,tc.secret) { t.Fatalf("leaked %q: %s",tc.secret,result) }
            if !strings.Contains(result,tc.marker) { t.Fatalf("missing redaction marker %q: %s",tc.marker,result) }
        })
    }
}

func TestIssue25ReportAndJournalRedactBeforeExport(t *testing.T) {
    store:=NewTestStore(t)
    app:=&App{store:store,bridge:NewBridge(context.Background(),store,func(string,...any){})}
    session:=store.OpenLogFile()
    app.bridge.logFile=session
    original:="link=https%3A%2F%2Fvk.com%2Fcall%2Fjoin%2FSecretJoinHash anonymName=SecretPerson device_id=SecretDevice"
    if err:=session.Write("DEBUG",original);err!=nil {t.Fatal(err)}
    snap:=session.Snapshot()
    for _, secret:=range []string{"SecretJoinHash","SecretPerson","SecretDevice"}{
        if strings.Contains(snap.content,secret){t.Fatalf("journal leaked %q: %q",secret,snap.content)}
    }
    report:=app.GenerateReport([]LogEntry{{Level:"DEBUG",Message:original,Time:"12:00"}})
    for _, secret:=range []string{"SecretJoinHash","SecretPerson","SecretDevice","- Hostname:","- Config:"}{
        if strings.Contains(report,secret){t.Fatalf("report leaked %q: %q",secret,report)}
    }
    if !strings.Contains(report,"Go:") || !strings.Contains(report,"Session log file"){
        t.Fatal("report lost diagnostic information")
    }
    _=session.Close()
}

func TestIssue25BridgeRedactsBeforeEmit(t *testing.T) {
    store:=NewTestStore(t)
    session:=store.OpenLogFile()
    defer session.Close()
    var emitted string
    bridge:=NewBridge(context.Background(),store,func(name string,args ...any){
        if name=="log" && len(args)>1 { emitted,_=args[1].(string) }
    })
    bridge.logFile=session
    bridge.recordLog("DEBUG","anonymName=SecretPerson device_id=SecretDevice",true)
    if strings.Contains(emitted,"SecretPerson") || strings.Contains(emitted,"SecretDevice") {
        t.Fatalf("unsanitized UI event: %q",emitted)
    }
    if !strings.Contains(emitted,"[redacted]") {t.Fatalf("missing redaction: %q",emitted)}
    if strings.Contains(session.Snapshot().content,"SecretPerson") {t.Fatal("journal stored secret")}
}
