package networkmanager

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/TTK4145/Network-go/network/bcast"
	"github.com/TTK4145/Network-go/network/localip"
	. "github.com/sanderfu/TTK4145-ElevatorProject/internal/channels"
	"github.com/sanderfu/TTK4145-ElevatorProject/internal/datatypes"
)

const (
	packetduplicates    = 10
	maxuniquesignatures = 25
	removeinclean       = int(maxuniquesignatures / 5)
)

var recentSignatures []string

var ip string

var start time.Time

var mode datatypes.NWMMode

//For some reason needs 1 in these struct{} channels to make it work
var killTransmitter = make(chan struct{}, 1)
var killReceiver = make(chan struct{}, 1)

var initTransmitter = make(chan struct{}, 1)
var initReceiver = make(chan struct{}, 1)

//NetworkManager to start networkmanager routine.
func NetworkManager() {
	//Start timer used for signatures
	start = time.Now()

	//Start networkWatch to detect connection loss (and switch to localhost)
	go networkWatch()

	//Initialize everything that need initializing
	recentSignatures = make([]string, 0)
	initTransmitter <- struct{}{}
	initReceiver <- struct{}{}
	InitDriverTX <- struct{}{}
	InitDriverRX <- struct{}{}
	mode = datatypes.Network
	ip, _ = localip.LocalIP()
	ip += ":" + strconv.Itoa(os.Getpid())

	for {
		select {
		case <-initTransmitter:
			go transmitter(16569)
		case <-initReceiver:
			go receiver(16569)
		}
	}
}

func networkWatch() {
	for {
		time.Sleep(1000 * time.Millisecond)
		theIP, err := localip.LocalIP()
		//fmt.Println("NetworkWatch checking state, the IP is", theIP)
		if err != nil {
			if mode != datatypes.Localhost {
				ip = "LOCALHOST" + ":" + strconv.Itoa(os.Getpid())
				mode = datatypes.Localhost
				killTransmitter <- struct{}{}
				killReceiver <- struct{}{}
			}
		} else {
			if mode != datatypes.Network {
				ip = theIP + ":" + strconv.Itoa(os.Getpid())
				mode = datatypes.Network
				killTransmitter <- struct{}{}
				killReceiver <- struct{}{}
			}
		}
	}
}

func createSignature(structType int) string {
	timeSinceStart := time.Since(start)
	t := strconv.FormatInt(timeSinceStart.Nanoseconds()/1e6, 10)
	senderIPStr := ip
	return senderIPStr + "@" + t + ":" + strconv.Itoa(structType)
}

func checkDuplicate(signature string) bool {
	for i := 0; i < len(recentSignatures); i++ {
		if recentSignatures[i] == signature {
			return true
		}
	}
	recentSignatures = append(recentSignatures, signature)
	if len(recentSignatures) > maxuniquesignatures {
		cleanArray()
	}
	return false
}

func cleanArray() {

	for i := 0; i < len(recentSignatures)-removeinclean; i++ {
		recentSignatures[i] = recentSignatures[i+removeinclean]
	}
	recentSignatures = recentSignatures[:len(recentSignatures)-removeinclean]
}

//TestSignatures tests that the signature system works as intended
func TestSignatures() {
	for i := 0; i < maxuniquesignatures*2; i++ {
		sign1 := createSignature(i)
		checkDuplicate(sign1)
		printRecentSignatures()
	}
}

func printRecentSignatures() {
	fmt.Println("")
	fmt.Println("Recentsignatures:")
	for j := 0; j < len(recentSignatures); j++ {
		fmt.Println(recentSignatures[j])
	}
}

//transmitter Function for applying packet redundancy before transmitting over network.
func transmitter(port int) {
	go bcast.Transmitter(port, mode, SWOrderTX, CostRequestTX, CostAnswerTX, OrderRecvAckTX, OrderCompleteTX)
	for {
		select {
		case order := <-SWOrderFOM:
			fmt.Println("Transmit swOrder")
			order.Signature = createSignature(0)
			order.SourceID = ip
			for i := 0; i < packetduplicates; i++ {
				SWOrderTX <- order
			}
		case costReq := <-CostRequestFOM:
			fmt.Println("Transmit costReq")
			costReq.Signature = createSignature(1)
			costReq.SourceID = ip
			for i := 0; i < packetduplicates; i++ {
				CostRequestTX <- costReq
			}
		case costAns := <-CostAnswerFOM:
			fmt.Println("Transmit costAns")
			costAns.Signature = createSignature(2)
			costAns.SourceID = ip
			for i := 0; i < packetduplicates; i++ {
				CostAnswerTX <- costAns
			}
		case orderRecvAck := <-OrderRecvAckFOM:
			fmt.Println("Transmit orderRecvAck")
			orderRecvAck.Signature = createSignature(3)
			orderRecvAck.SourceID = ip
			for i := 0; i < packetduplicates; i++ {
				OrderRecvAckTX <- orderRecvAck
			}
		case orderComplete := <-OrderCompleteFOM:
			fmt.Println("Transmit orderComplete")
			orderComplete.Signature = createSignature(4)
			for i := 0; i < packetduplicates; i++ {
				OrderCompleteTX <- orderComplete
			}
		case <-killTransmitter:
			KillDriverTX <- struct{}{}
			initTransmitter <- struct{}{}
			return
		}
	}
}

func receiver(port int) {
	go bcast.Receiver(port, SWOrderRX, CostRequestRX, CostAnswerRX, OrderRecvAckRX, OrderCompleteRX)
	for {
		select {
		case order := <-SWOrderRX:
			if ip != order.PrimaryID && ip != order.BackupID {
				//We are not part of this order, ignore it
				continue
			}
			if !checkDuplicate(order.Signature) {
				if order.PrimaryID == ip {
					SWOrderTOMPrimary <- order
				} else {
					SWOrderTOMBackup <- order
				}
			}
		case costReq := <-CostRequestRX:
			if !checkDuplicate(costReq.Signature) {
				CostRequestTOM <- costReq
			}
		case costAns := <-CostAnswerRX:
			if costAns.DestinationID != ip {
				continue
			}
			if !checkDuplicate(costAns.Signature) {
				CostAnswerTOM <- costAns
			}
		case orderRecvAck := <-OrderRecvAckRX:
			if orderRecvAck.DestinationID != ip {
				continue
			}
			if !checkDuplicate(orderRecvAck.Signature) {
				OrderRecvAckTOM <- orderRecvAck
			}
		case orderComplete := <-OrderCompleteRX:
			if !checkDuplicate(orderComplete.Signature) {
				OrderCompleteTOM <- orderComplete
			}
		case <-killReceiver:
			KillDriverRX <- struct{}{}
			initReceiver <- struct{}{}
			return
		}
	}
}

//TestSending Function to test basic order transmission over network
func TestSending() {
	for {
		var testOrdre datatypes.SWOrder
		testOrdre.PrimaryID = "12345"
		testOrdre.BackupID = "67890"
		testOrdre.Dir = datatypes.INSIDE
		testOrdre.Floor = datatypes.SECOND
		SWOrderTX <- testOrdre
		time.Sleep(1 * time.Second)
	}
}

//TestRecieving Function to test basic order transmission over network
func TestRecieving() {
	for {
		select {
		case order := <-SWOrderRX:
			fmt.Printf("Received: %#v\n", order)

		}
	}
}
