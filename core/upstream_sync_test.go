package core

import (
    "context"
    "errors"
    "fmt"
    "sync"
    "sync/atomic"
    "testing"
)

func TestSelectTurnURLRotationAndBannedRelay(t *testing.T) {
    urls := []string{"relay-a", "relay-b", "relay-c"}
    blocked := func(s string) bool { return s == "relay-b" }
    tests := []struct{worker, offset int; want string}{
        {1,0,"relay-a"}, {2,0,"relay-c"}, {1,1,"relay-c"},
        {3,0,"relay-c"}, {1,3,"relay-a"},
    }
    for _, tt := range tests {
        got, ok := selectTurnURL(urls, tt.worker, tt.offset, blocked)
        if !ok || got != tt.want { t.Fatalf("worker=%d offset=%d got=%s ok=%v want=%s",tt.worker,tt.offset,got,ok,tt.want) }
    }
    if _, ok := selectTurnURL(urls, 1, 0, func(string) bool { return true }); ok { t.Fatal("accepted banned relay") }
    if _, ok := selectTurnURL(nil, 1, 0, blocked); ok { t.Fatal("accepted empty relays") }
}

func TestConfigRacersAreBounded(t *testing.T) {
    var inFlight int32
    const workers = 64
    start := make(chan struct{})
    var wg sync.WaitGroup
    var granted atomic.Int32
    for i:=0;i<workers;i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            <- start
            if tryAcquireConfigRacer(&inFlight) { granted.Add(1) }
        }()
    }
    close(start)
    wg.Wait()
    if got:=granted.Load(); got!=configRacerLimit { t.Fatalf("granted=%d; want=%d", got, configRacerLimit) }
    if got:=atomic.LoadInt32(&inFlight); got!=configRacerLimit { t.Fatalf("inFlight=%d",got) }
    atomic.AddInt32(&inFlight,-1)
    if !tryAcquireConfigRacer(&inFlight) { t.Fatal("slot not reusable") }
}

func TestRetryOnlyVKCallsStepFiveOutdated(t *testing.T) {
    outdated := errors.New("error.webrtc.auth.anonym_token.outdated")
    cases := []struct{name string; err error; want bool}{
        {"step 5", newVKCallsFailure("step5 vchat.joinConversationByLink",vkCallsFailureOKCDN,outdated),true},
        {"wrong step",newVKCallsFailure("step4 auth.anonymLogin",vkCallsFailureOKCDN,outdated),false},
        {"wrong kind",newVKCallsFailure("step5 vchat.joinConversationByLink",vkCallsFailureVKAPI,outdated),false},
        {"rate limit",newVKCallsFailure("step5 vchat.joinConversationByLink",vkCallsFailureOKCDN,errors.New("rate limit")),false},
        {"untyped",outdated,false},
    }
    for _,tt:=range cases {t.Run(tt.name,func(t *testing.T){
        if got:=shouldRetryVKCallsOutdatedToken(tt.err);got!=tt.want {t.Fatalf("got %v want %v",got,tt.want)}
    })}
    if err:=context.Canceled;shouldRetryVKCallsOutdatedToken(fmt.Errorf("cancel: %w",err)) {t.Fatal("cancel should not retry")}
}

func TestVKCallsOutdatedRetryAtMostOnce(t *testing.T) {
    outdated := newVKCallsFailure("step5 vchat.joinConversationByLink", vkCallsFailureOKCDN,
        errors.New("error.webrtc.auth.anonym_token.outdated"))
    cases := []struct{name string; first, second error; wantCalls int; wantError bool}{
        {"outdated then success",outdated,nil,2,false},
        {"outdated again",outdated,outdated,2,true},
        {"captcha",newVKCallsFailure("step5 vchat.joinConversationByLink",vkCallsFailureCaptcha,errors.New("CAPTCHA")),nil,1,true},
        {"network",newVKCallsFailure("step5 vchat.joinConversationByLink",vkCallsFailureNetwork,errors.New("network")),nil,1,true},
        {"fatal call",newVKCallsFailure("step5 vchat.joinConversationByLink",vkCallsFailureCall,errors.New("call unavailable")),nil,1,true},
    }
    for _,tt:=range cases {t.Run(tt.name,func(t *testing.T){
        calls:=0
        user,_,_,retried,err:=retryVKCallsOutdatedToken(context.Background(),0,func()(string,string,[]string,error){
            calls++
            if calls==1 {return "", "", nil, tt.first}
            return "alice","pass",[]string{"127.0.0.1:3478"},tt.second
        })
        if calls!=tt.wantCalls || (err!=nil)!=tt.wantError || retried!=(tt.wantCalls==2) {
            t.Fatalf("calls=%d retried=%v err=%v",calls,retried,err)
        }
        if err==nil && user!="alice" {t.Fatalf("success user=%q",user)}
    })}
}
