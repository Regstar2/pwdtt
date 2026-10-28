package backend_test

import (
    "encoding/json"
    "testing"

    "pwdtt/backend"
)

func TestUpstreamProfileRoundtripAndLegacy(t *testing.T) {
    store := backend.NewTestStore(t)
    original := backend.ProfileData{
        PeerAddr: "example.org:56000", Password:"secret", Hashes:[]string{"hash"},
        Workers:18, TurnTCP:true, DeviceID:"stable-device",
        HashPolicy:&backend.VKHashPolicy{Mode:"local", AutoCheck:true},
    }
    if err:=store.SaveProfile("Example",original);err!=nil {t.Fatal(err)}
    got,err:=store.LoadProfile("Example")
    if err!=nil {t.Fatal(err)}
    if got.Workers!=18 || !got.TurnTCP || got.DeviceID!="stable-device" || got.HashPolicy==nil || !got.HashPolicy.AutoCheck {
        t.Fatalf("profile values lost: %+v",got)
    }
    if err:=store.SaveProfile("Example",backend.ProfileData{PeerAddr:original.PeerAddr,Workers:27});err!=nil {t.Fatal(err)}
    got,err=store.LoadProfile("Example")
    if err!=nil {t.Fatal(err)}
    if got.DeviceID!="stable-device" || got.HashPolicy==nil || got.Workers!=27 {t.Fatalf("preservation failed: %+v",got)}
    var legacy backend.ProfileData
    if err:=json.Unmarshal([]byte(`{"peer":"127.0.0.1:56000","password":"x","device_id":"legacy"}`),&legacy);err!=nil {t.Fatal(err)}
    if legacy.TurnTCP || legacy.Workers!=0 || legacy.DeviceID!="legacy" {t.Fatalf("bad legacy defaults: %+v",legacy)}
}
