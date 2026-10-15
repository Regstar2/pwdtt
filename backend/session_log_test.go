package backend

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "sync"
 "testing"
)

func TestIssue44LiveSnapshotBelowSyncThreshold(t *testing.T) {
 store := NewTestStore(t)
 session := store.OpenLogFile()
 for i:=0; i<12; i++ {
  if err:=session.Write("INFO","connection event"); err!=nil { t.Fatal(err) }
 }
 snapshot:=session.Snapshot()
 if strings.Count(snapshot.content,"connection event")!=12 { t.Fatalf("live snapshot lost events: %q",snapshot.content) }
 if snapshot.fallback || len(snapshot.warnings)!=0 { t.Fatalf("unexpected fallback: %+v",snapshot) }
 if err:=session.Close(); err!=nil { t.Fatal(err) }
}

func TestIssue44EmptyAndUIPreflightFallback(t *testing.T) {
 store:=NewTestStore(t)
 app:=&App{store:store,bridge:NewBridge(context.Background(),store,func(string,...any){})}
 if report:=app.GenerateReport(nil); !strings.Contains(report,"(no session events)") { t.Fatalf("missing empty state: %q",report) }
 report:=app.GenerateReport([]LogEntry{{Level:"ERROR",Time:"12:00",Message:"preflight failed"}})
 if !strings.Contains(report,"preflight failed") || !strings.Contains(report,"fallback") { t.Fatalf("missing UI fallback: %q",report) }
}

func TestIssue44OpenFailureIsVisible(t *testing.T) {
 store:=NewTestStore(t)
 dir:=filepath.Join(store.baseDir,"logs")
 if err:=os.RemoveAll(dir); err!=nil { t.Fatal(err) }
 if err:=os.WriteFile(dir,[]byte("blocked"),0o600); err!=nil { t.Fatal(err) }
 session:=store.OpenLogFile()
 if err:=session.Write("INFO","memory event"); err==nil { t.Fatal("expected I/O error") }
 snap:=session.Snapshot()
 if !snap.fallback || !strings.Contains(snap.content,"memory event") || len(snap.warnings)==0 { t.Fatalf("missing failure fallback: %+v",snap) }
 app:=&App{store:store,bridge:NewBridge(context.Background(),store,func(string,...any){})}
 app.bridge.logFile=session
 report:=app.GenerateReport(nil)
 if !strings.Contains(report,"Warning:") || !strings.Contains(report,"memory event") { t.Fatalf("missing warning: %q",report) }
}

func TestIssue44WriteFailureUsesMemory(t *testing.T) {
 store:=NewTestStore(t)
 session:=store.OpenLogFile()
 if err:=session.file.Close(); err!=nil { t.Fatal(err) }
 if err:=session.Write("ERROR","failed write event"); err==nil { t.Fatal("expected Write error") }
 snap:=session.Snapshot()
 if !snap.fallback || !strings.Contains(snap.content,"failed write event") || len(snap.warnings)==0 { t.Fatalf("missing fallback/error: %+v",snap) }
}

func TestIssue44ReadFailureUsesMemory(t *testing.T) {
 store:=NewTestStore(t)
 session:=store.OpenLogFile()
 if err:=session.Write("INFO","read fails after write"); err!=nil { t.Fatal(err) }
 if err:=os.Remove(session.path); err!=nil { t.Fatal(err) }
 snap:=session.Snapshot()
 if !snap.fallback || !strings.Contains(snap.content,"read fails after write") || len(snap.warnings)==0 { t.Fatalf("missing read fallback: %+v",snap) }
 _=session.Close()
}

func TestIssue44LastClosedSessionNotUnrelatedLatestFile(t *testing.T) {
 store:=NewTestStore(t)
 previous:=store.OpenLogFile()
 if err:=previous.Write("INFO","prior session"); err!=nil { t.Fatal(err) }
 _=previous.Close()
 current:=store.OpenLogFile()
 if err:=current.Write("INFO","selected session"); err!=nil { t.Fatal(err) }
 _=current.Close()
 if err:=os.WriteFile(filepath.Join(store.baseDir,"logs","zzzz-unrelated.log"),[]byte("wrong session"),0o600); err!=nil { t.Fatal(err) }
 app:=&App{store:store,bridge:NewBridge(context.Background(),store,func(string,...any){})}
 app.bridge.logFile=current
 report:=app.GenerateReport(nil)
 if !strings.Contains(report,"selected session") || strings.Contains(report,"wrong session") || strings.Contains(report,"prior session") { t.Fatalf("wrong session: %q",report) }
}

func TestIssue44ConcurrentWritesSnapshotAndClose(t *testing.T) {
 store:=NewTestStore(t)
 session:=store.OpenLogFile()
 var wg sync.WaitGroup
 for worker:=0;worker<4;worker++{
  wg.Add(1)
  go func(){defer wg.Done();for i:=0;i<200;i++{_ = session.Write("DEBUG","parallel event");_ = session.Snapshot()}}()
 }
 wg.Add(1)
 go func(){defer wg.Done();_ = session.Close()}()
 wg.Wait()
 if session.Snapshot().content=="" { t.Fatal("concurrent snapshot lost all events") }
}

func TestIssue44BoundedLogAndRetention(t *testing.T) {
 store:=NewTestStore(t)
 for i:=0;i<sessionLogRetention+5;i++{
  session:=store.OpenLogFile()
  if err:=session.Write("INFO","older");err!=nil {t.Fatal(err)}
  _=session.Close()
 }
 dir:=filepath.Join(store.baseDir,"logs")
 entries,err:=os.ReadDir(dir)
 if err!=nil {t.Fatal(err)}
 if len(entries)>sessionLogRetention {t.Fatalf("retention limit exceeded: %d",len(entries))}
 session:=store.OpenLogFile()
 for i:=0;i<500;i++{if err:=session.Write("INFO",strings.Repeat("x",3000));err!=nil {t.Fatal(err)}}
 snap:=session.Snapshot()
 if session.ringBytes>sessionLogMemoryLimit || session.size>sessionLogFileLimit {t.Fatal("size limit exceeded")}
 if !snap.fallback || len(snap.warnings)==0 {t.Fatalf("missing truncation warning: %+v",snap)}
 _=session.Close()
}

func TestIssue44ReportRedaction(t *testing.T) {
 store:=NewTestStore(t)
 session:=store.OpenLogFile()
 sensitive:=[]string{
  "https://vk.com/call/join/PrivateCallHash?access_token=SecretToken",
  "password=abc123",
  `{"username":"Alice Smith","user_id":123456}`,
  `C:\Users\Alice Smith\AppData\Roaming\pwdtt`,
  "/home/alice/.config/pwdtt",
  "?custom_argument=private_value",
 }
 for _,line:=range sensitive {if err:=session.Write("DEBUG",line);err!=nil {t.Fatal(err)}}
 app:=&App{store:store,bridge:NewBridge(context.Background(),store,func(string,...any){})}
 app.bridge.logFile=session
 report:=app.GenerateReport([]LogEntry{{Level:"INFO",Message:strings.Join(sensitive," "),Time:"12:00"}})
 for _,secret:=range []string{"PrivateCallHash","SecretToken","abc123","Alice Smith","123456",`C:\Users\Alice`,"/home/alice","private_value","- Hostname:","- Config:"} {
  if strings.Contains(report,secret) {t.Errorf("report leaked %q",secret)}
 }
 if !strings.Contains(report,"[link redacted]") || !strings.Contains(report,"[path redacted]") {t.Fatalf("markers missing: %q",report)}
 _=session.Close()
}
