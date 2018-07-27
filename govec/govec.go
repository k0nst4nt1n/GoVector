package govec

import (
	"bytes"
	"fmt"
	"github.com/DistributedClocks/GoVector/govec/vclock"
	"github.com/daviddengcn/go-colortext"
	"github.com/vmihailenco/msgpack"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

/*
   - All licences like other licenses ...

   How to Use This Library

   Step 1:
   Create a Global Variable and Initialize it using like this =

   Logger:= Initialize("MyProcess",ShouldYouSeeLoggingOnScreen,ShouldISendVectorClockonWire,Debug)

   Step 2:
   When Ever You Decide to Send any []byte , before sending call PrepareSend like this:
   SENDSLICE := PrepareSend("Message Description", YourPayload)
   and send the SENDSLICE instead of your Designated Payload

   Step 3:
   When Receiveing, AFTER you receive your message, pass the []byte into UnpackRecieve
   like this:

   UnpackReceive("Message Description", []ReceivedPayload, *RETURNSLICE)
   and use RETURNSLICE for further processing.
*/

var (
	logToTerminal                       = false
	_             msgpack.CustomEncoder = (*ClockPayload)(nil)
	_             msgpack.CustomDecoder = (*ClockPayload)(nil)
)

type LogPriority int

//LogPriority enum provides all the valid Priority Levels that can be
//used to log events with.
const (
	DEBUG LogPriority = iota
	INFO
	WARNING
	ERROR
	FATAL
)

func (l LogPriority) getColor() ct.Color {
	var color ct.Color
	switch l {
	case DEBUG:
		color = ct.Green
	case INFO:
		color = ct.White
	case WARNING:
		color = ct.Yellow
	case ERROR:
		color = ct.Red
	case FATAL:
		color = ct.Magenta
	default:
		color = ct.None
	}
	return color
}

func (l LogPriority) getPrefixString() string {
	var prefix string
	switch l {
	case DEBUG:
		prefix = "DEBUG"
	case INFO:
		prefix = "NORMAL"
	case WARNING:
		prefix = "WARNING"
	case ERROR:
		prefix = "ERROR"
	case FATAL:
		prefix = "FATAL"
	default:
		prefix = ""
	}
	return prefix
}

type GoLogConfig struct {
	Buffered         bool
	PrintOnScreen    bool
	AppendLog        bool
	UseTimestamps    bool
	EncodingStrategy func(interface{}) ([]byte, error)
	DecodingStrategy func([]byte, interface{}) error
	LogToFile        bool
	Priority         LogPriority
}

//Returns the default GoLogConfig with default values for various fields.
func GetDefaultConfig() GoLogConfig {
	config := GoLogConfig{Buffered: false, PrintOnScreen: false, AppendLog: false, UseTimestamps: false, LogToFile: true, Priority: INFO}
	return config
}

//This is the data structure that is actually end on the wire
type ClockPayload struct {
	Pid     string
	VcMap   map[string]uint64
	Payload interface{}
}

//Prints the Data Stuct as Bytes
func (d *ClockPayload) PrintDataBytes() {
	fmt.Printf("%x \n", d.Pid)
	fmt.Printf("%X \n", d.VcMap)
	fmt.Printf("%X \n", d.Payload)
}

//Prints the Data Struct as a String
func (d *ClockPayload) String() (s string) {
	s += "-----DATA START -----\n"
	s += string(d.Pid[:])
	s += "-----DATA END -----\n"
	return
}

/* Custom encoder function, needed for msgpack interoperability */
func (d *ClockPayload) EncodeMsgpack(enc *msgpack.Encoder) error {

	var err error

	err = enc.EncodeString(d.Pid)
	if err != nil {
		return err
	}

	err = enc.Encode(d.Payload)
	if err != nil {
		return err
	}

	err = enc.EncodeMapLen(len(d.VcMap))
	if err != nil {
		return err
	}

	for key, value := range d.VcMap {

		err = enc.EncodeString(key)
		if err != nil {
			return err
		}

		err = enc.EncodeUint(value)
		if err != nil {
			return err
		}
	}

	return nil

}

/* Custom decoder function, needed for msgpack interoperability */
func (d *ClockPayload) DecodeMsgpack(dec *msgpack.Decoder) error {
	var err error

	pid, err := dec.DecodeString()
	if err != nil {
		return err
	}
	d.Pid = pid

	err = dec.Decode(&d.Payload)
	if err != nil {
		return err
	}

	mapLen, err := dec.DecodeMapLen()
	if err != nil {
		return err
	}
	var vcMap map[string]uint64
	vcMap = make(map[string]uint64)

	for i := 0; i < mapLen; i++ {

		key, err := dec.DecodeString()
		if err != nil {
			return err
		}

		value, err := dec.DecodeUint64()
		if err != nil {
			return err
		}
		vcMap[key] = value
	}
	err = dec.Decode(&d.Pid, &d.Payload, &d.VcMap)
	d.VcMap = vcMap
	if err != nil {
		return err
	}

	return nil
}

//The GoLog struct provides an interface to creating and maintaining
//vector timestamp entries in the generated log file
type GoLog struct {

	//Local Process ID
	pid string

	//Local vector clock in bytes
	currentVC vclock.VClock

	//Flag to Printf the logs made by Local Program
	printonscreen bool

	//If true GoLog will write to a file
	logging bool

	//If true logs are buffered in memory and flushed to disk upon
	//calling flush. Logs can be lost if a program stops prior to
	//flushing buffered logs.
	buffered bool

	//Flag to include timestamps when logging events
	usetimestamps bool

	//Flag to indicate if the log file will contain multiple executions
	appendLog bool

	//Priority level at which all events are logged
	priority LogPriority

	//Logfile name
	logfile string

	//buffered string
	output string

	encodingStrategy func(interface{}) ([]byte, error)
	decodingStrategy func([]byte, interface{}) error

	logger *log.Logger

	mutex sync.RWMutex
}

//Returns a Go Log Struct taking in two arguments and truncates previous logs:
//MyProcessName (string): local process name; must be unique in your distributed system.
//LogFileName (string) : name of the log file that will store info. Any old log with the same name will be truncated
//Config (GoLogConfig) : config struct defining the values of the options of GoLog logger
func InitGoVector(processid string, logfilename string, config GoLogConfig) *GoLog {

	gv := &GoLog{}
	gv.pid = processid

	if logToTerminal {
		gv.logger = log.New(os.Stdout, "[GoVector]:", log.Lshortfile)
	} else {
		var buf bytes.Buffer
		gv.logger = log.New(&buf, "[GoVector]:", log.Lshortfile)
	}

	gv.printonscreen = config.PrintOnScreen
	gv.usetimestamps = config.UseTimestamps
	gv.priority = config.Priority
	gv.logging = config.LogToFile
	gv.buffered = config.Buffered
	gv.appendLog = config.AppendLog
	gv.output = ""

	// Use the default encoder/decoder. As of July 2017 this is msgPack.
	if config.EncodingStrategy == nil || config.DecodingStrategy == nil {
		gv.setEncoderDecoder(gv.DefaultEncoder, gv.DefaultDecoder)
	} else {
		gv.setEncoderDecoder(config.EncodingStrategy, config.DecodingStrategy)
	}
	print(config.EncodingStrategy)

	//we create a new Vector Clock with processname and 0 as the intial time
	vc1 := vclock.New()
	vc1.Tick(processid)
	gv.currentVC = vc1

	//Starting File IO . If Log exists, Log Will be deleted and A New one will be created
	logname := logfilename + "-Log.txt"
	gv.logfile = logname
	gv.prepareLogFile()

	return gv
}

func (gv *GoLog) prepareLogFile() {
	_, err := os.Stat(gv.logfile)
	if err == nil {
		if !gv.appendLog {
			gv.logger.Println(gv.logfile, "exists! ... Deleting ")
			os.Remove(gv.logfile)
		} else {
			executionnumber := time.Now().Format(time.UnixDate)
			gv.logger.Println("Execution Number is  ", executionnumber)
			executionstring := "=== Execution #" + executionnumber + "  ==="
			gv.logThis(executionstring, "", "", gv.priority)
			return
		}
	}
	// Create directory path to log if it doesn't exist.
	if err := os.MkdirAll(filepath.Dir(gv.logfile), 0750); err != nil {
		gv.logger.Println(err)
	}

	//Creating new Log
	file, err := os.Create(gv.logfile)
	if err != nil {
		gv.logger.Println(err)
	}

	file.Close()

	if gv.appendLog {
		executionnumber := time.Now().Format(time.UnixDate)
		gv.logger.Println("Execution Number is  ", executionnumber)
		executionstring := "=== Execution #" + executionnumber + "  ==="
		gv.logThis(executionstring, "", "", gv.priority)
	}

	ok := gv.logThis("Initialization Complete", gv.pid, gv.currentVC.ReturnVCString(), gv.priority)
	if ok == false {
		gv.logger.Println("Something went Wrong, Could not Log!")
	}
}

//Returns the current vector clock
func (gv *GoLog) GetCurrentVC() vclock.VClock {
	return gv.currentVC
}

//Sets the Encoding and Decoding functions which are to be used by the logger
//Encoder (func(interface{}) ([]byte, error)) : function to be used for encoding
//Decoder (func([]byte, interface{}) error) : function to be used for decoding
func (gv *GoLog) setEncoderDecoder(encoder func(interface{}) ([]byte, error), decoder func([]byte, interface{}) error) {
	gv.encodingStrategy = encoder
	gv.decodingStrategy = decoder
}

func (gv *GoLog) DefaultEncoder(payload interface{}) ([]byte, error) {
	return msgpack.Marshal(payload)
}

func (gv *GoLog) DefaultDecoder(buf []byte, payload interface{}) error {
	return msgpack.Unmarshal(buf, payload)
}

//Enables buffered writes to the log file. All the log messages are only written
//to the LogFile via an explicit call to the function Flush.
//Note: Buffered writes are automatically disabled.
func (gv *GoLog) EnableBufferedWrites() {
	gv.buffered = true
}

//Disables buffered writes to the log file. All the log messages from now on
//will be written to the Log file immediately. Writes all the existing
//log messages that haven't been written to Log file yet.
func (gv *GoLog) DisableBufferedWrites() {
	gv.buffered = false
	if gv.output != "" {
		gv.Flush()
	}
}

//Writes the log messages stored in the buffer to the Log File. This
//function should be used by the application to also force writes in
//the case of interrupts and crashes.   Note: Calling Flush when
//BufferedWrites is disabled is essentially a no-op.
func (gv *GoLog) Flush() bool {
	complete := true
	file, err := os.OpenFile(gv.logfile, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		complete = false
	}
	defer file.Close()

	if _, err = file.WriteString(gv.output); err != nil {
		complete = false
	}

	gv.output = ""
	return complete
}

func (gv *GoLog) printColoredMessage(LogMessage string, Priority LogPriority) {
	color := Priority.getColor()
	prefix := Priority.getPrefixString()
	ct.Foreground(color, true)
	fmt.Print(time.Now().Format(time.UnixDate) + ":" + prefix + "-")
	ct.ResetColor()
	fmt.Println(LogMessage)
}

//Logs a message along with a processID and a vector clock, the VCString
//must be a valid vector clock, true is returned on success
func (gv *GoLog) logThis(Message string, ProcessID string, VCString string, Priority LogPriority) bool {
	var (
		complete = true
		buffer   bytes.Buffer
	)
	if gv.usetimestamps {
		buffer.WriteString(strconv.FormatInt(time.Now().UnixNano(), 10))
		buffer.WriteString(" ")
	}
	buffer.WriteString(ProcessID)
	buffer.WriteString(" ")
	buffer.WriteString(VCString)
	buffer.WriteString("\n")
	buffer.WriteString(Message)
	buffer.WriteString("\n")
	output := buffer.String()

	gv.output += output
	if !gv.buffered {
		complete = gv.Flush()
	}

	if gv.printonscreen == true {
		gv.printColoredMessage(Message, Priority)
	}
	return complete

}

func (gv *GoLog) logWriteWrapper(logMessage, errorMessage string, Priority LogPriority) (success bool) {
	if gv.logging == true {
		success = gv.logThis(logMessage, gv.pid, gv.currentVC.ReturnVCString(), Priority)
		if !success {
			gv.logger.Println(errorMessage)
		}
	}
	return
}

func (gv *GoLog) tickClock() {
	_, found := gv.currentVC.FindTicks(gv.pid)
	if !found {
		gv.logger.Println("Couldn't find this process's id in its own vector clock!")
	}
	gv.currentVC.Tick(gv.pid)
}

//Increments current vector timestamp and logs it into Log File.
//* LogMessage (string) : Message to be logged
func (gv *GoLog) LogLocalEvent(Message string) (logSuccess bool) {
	return gv.LogLocalEventWithPriority(Message, gv.priority)
}

//If the priority of the logger is lower than or equal to the priority
//of this event then the current vector timestamp is incremented and the
//message is logged it into the Log File. A color coded string is also
//printed on the console.
//* LogMessage (string) : Message to be logged
//* Priority (LogPriority) : Priority at which the message is to be logged
func (gv *GoLog) LogLocalEventWithPriority(LogMessage string, Priority LogPriority) (logSuccess bool) {
	logSuccess = true
	gv.mutex.Lock()
	if Priority >= gv.priority {
		prefix := Priority.getPrefixString() + " - "
		gv.tickClock()
		logSuccess = gv.logWriteWrapper(prefix+LogMessage, "Something went Wrong, Could not Log LocalEvent!", Priority)
	}
	gv.mutex.Unlock()
	return
}

/*
This function is meant to be used immediately before sending.
LogMessage will be logged along with the time of the send
buf is encode-able data (structure or basic type)
Returned is an encoded byte array with logging information.

This function is meant to be called before sending a packet. Usually,
it should Update the Vector Clock for its own process, package with
the clock using gob support and return the new byte array that should
be sent onwards using the Send Command
*/
func (gv *GoLog) PrepareSendWithPriority(mesg string, buf interface{}, Priority LogPriority) (encodedBytes []byte) {

	//Converting Vector Clock from Bytes and Updating the gv clock
	gv.mutex.Lock()
	if Priority >= gv.priority {
		gv.tickClock()

		gv.logWriteWrapper(mesg, "Something went wrong, could not log prepare send", Priority)

		d := ClockPayload{Pid: gv.pid, VcMap: gv.currentVC.GetMap(), Payload: buf}

		// encode the Clock Payload
		var err error
		encodedBytes, err = gv.encodingStrategy(&d)
		if err != nil {
			gv.logger.Println(err.Error())
		}

		// return encodedBytes which can be sent off and received on the other end!
		gv.mutex.Unlock()
	}
	return
}

/*
This function is meant to be used immediately before sending.
LogMessage will be logged along with the time of the send
buf is encode-able data (structure or basic type)
Returned is an encoded byte array with logging information.

This function is meant to be called before sending a packet. Usually,
it should Update the Vector Clock for its own process, package with
the clock using gob support and return the new byte array that should
be sent onwards using the Send Command
*/
func (gv *GoLog) PrepareSend(mesg string, buf interface{}) []byte {
	return gv.PrepareSendWithPriority(mesg, buf, gv.priority)
}

func (gv *GoLog) mergeIncomingClock(mesg string, e ClockPayload, Priority LogPriority) {

	// First, tick the local clock
	gv.tickClock()
	gv.currentVC.Merge(e.VcMap)

	gv.logWriteWrapper(mesg, "Something went Wrong, Could not Log!", Priority)
}

/*
UnpackReceive is used to unmarshall network data into local structures.
LogMessage will be logged along with the vector time the receive happened
buf is the network data, previously packaged by PrepareSend unpack is
a pointer to a structure, the same as was packed by PrepareSend

This function is meant to be called immediately after receiving
a packet. It unpacks the data by the program, the vector clock. It
updates vector clock and logs it. and returns the user data
*/
func (gv *GoLog) UnpackReceiveWithPriority(mesg string, buf []byte, unpack interface{}, Priority LogPriority) {

	gv.mutex.Lock()

	if Priority >= gv.priority {
		e := ClockPayload{}
		e.Payload = unpack

		// Just use msgpack directly
		err := gv.decodingStrategy(buf, &e)
		if err != nil {
			gv.logger.Println(err.Error())
		}

		// Increment and merge the incoming clock
		gv.mergeIncomingClock(mesg, e, Priority)
		gv.mutex.Unlock()
	}

}

/*
UnpackReceive is used to unmarshall network data into local structures.
LogMessage will be logged along with the vector time the receive happened
buf is the network data, previously packaged by PrepareSend unpack is
a pointer to a structure, the same as was packed by PrepareSend

This function is meant to be called immediately after receiving
a packet. It unpacks the data by the program, the vector clock. It
updates vector clock and logs it. and returns the user data
*/
func (gv *GoLog) UnpackReceive(mesg string, buf []byte, unpack interface{}) {
	gv.UnpackReceiveWithPriority(mesg, buf, unpack, gv.priority)
}
