package core

import (
    "context"
    "net"
    "testing"
    "time"

    "github.com/pion/turn/v5"
)

// A self-contained TURN server verifies both client transports. No VK
// credentials, external TURN/VPS or real WireGuard configuration is required.
func TestIssue58TURNUDPAndTCPAllocatePermissionRelay(t *testing.T) {
    for _, transport := range []struct{name string; tcp bool}{
        {"udp",false}, {"tcp",true},
    } {
        t.Run(transport.name,func(t *testing.T) {
            udpListener,err:=net.ListenPacket("udp4","127.0.0.1:0")
            if err!=nil {t.Fatal(err)}
            tcpListener,err:=net.Listen("tcp4","127.0.0.1:0")
            if err!=nil {udpListener.Close();t.Fatal(err)}

            const realm = "pwdtt-local-test"
            generator:=func() *turn.RelayAddressGeneratorStatic {
                return &turn.RelayAddressGeneratorStatic{
                    RelayAddress: net.IPv4(127,0,0,1),
                    Address: "127.0.0.1",
                }
            }
            server,err:=turn.NewServer(turn.ServerConfig{
                Realm:realm,
                AuthHandler:func(ra *turn.RequestAttributes)(string,[]byte,bool){
                    if ra.Username!="alice" {return "",nil,false}
                    return ra.Username,turn.GenerateAuthKey(ra.Username,realm,"secret"),true
                },
                PacketConnConfigs:[]turn.PacketConnConfig{
                    {PacketConn:udpListener,RelayAddressGenerator:generator()},
                },
                ListenerConfigs:[]turn.ListenerConfig{
                    {Listener:tcpListener,RelayAddressGenerator:generator()},
                },
            })
            if err!=nil {udpListener.Close();tcpListener.Close();t.Fatal(err)}
            defer server.Close()

            peer,err:=net.ListenPacket("udp4","127.0.0.1:0")
            if err!=nil {t.Fatal(err)}
            defer peer.Close()

            address:=udpListener.LocalAddr().String()
            if transport.tcp {address=tcpListener.Addr().String()}
            ctx,cancel:=context.WithTimeout(context.Background(),8*time.Second)
            defer cancel()
            socket,err:=dialTURNConn(ctx,address,transport.tcp)
            if err!=nil {t.Fatal(err)}
            defer socket.Close()

            client,err:=turn.NewClient(&turn.ClientConfig{
                STUNServerAddr:address, TURNServerAddr:address,
                Conn:socket,Username:"alice",Password:"secret",
                RequestedAddressFamily:turn.RequestedAddressFamilyIPv4,
                LoggerFactory:&NullLoggerFactory{},
            })
            if err!=nil {t.Fatal(err)}
            defer client.Close()
            if err:=client.Listen();err!=nil {t.Fatal(err)}
            relay,err:=client.Allocate()
            if err!=nil {t.Fatalf("Allocate: %v",err)}
            defer relay.Close()

            if err:=client.CreatePermission(peer.LocalAddr());err!=nil {
                t.Fatalf("CreatePermission: %v",err)
            }
            if _,err:=relay.WriteTo([]byte("ping"),peer.LocalAddr());err!=nil {
                t.Fatalf("relay WriteTo: %v",err)
            }
            buf:=make([]byte,256)
            if err:=peer.SetReadDeadline(time.Now().Add(3*time.Second));err!=nil {t.Fatal(err)}
            n,from,err:=peer.ReadFrom(buf)
            if err!=nil {t.Fatalf("peer ReadFrom: %v",err)}
            if string(buf[:n])!="ping" {t.Fatalf("peer received %q",buf[:n])}
            if _,err:=peer.WriteTo([]byte("pong"),from);err!=nil {t.Fatal(err)}
            if err:=relay.SetReadDeadline(time.Now().Add(3*time.Second));err!=nil {t.Fatal(err)}
            n,_,err=relay.ReadFrom(buf)
            if err!=nil {t.Fatalf("relay ReadFrom: %v",err)}
            if string(buf[:n])!="pong" {t.Fatalf("relay received %q",buf[:n])}
        })
    }
}
