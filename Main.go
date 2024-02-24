package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/go-errors/errors"
)

type ValueRange[T ~int | ~int32 | ~int64 | ~uint | ~uint32 | ~uint64 | ~float32 | ~float64] struct {
	MinimumValue T
	MaximumValue T
}

func (Arg_Range ValueRange[T]) IsValid() bool {
	return Arg_Range.MinimumValue < Arg_Range.MaximumValue
}

func (Arg_Range ValueRange[T]) Contains(Arg_Value T) bool {
	return Arg_Range.MinimumValue <= Arg_Value && Arg_Range.MaximumValue >= Arg_Value
}

/* type SensorValue[T ~int | ~int32 | ~int64 | ~float32 | ~float64] struct {
    ValueRange ValueRange[T]
    __GetValue func(*Sensor) T
} */

type Sensor struct {
	Name         string
	FriendlyName string
	ValueUnit    string
	ValueRange   any
	Data         any
	__GetValue   func(*Sensor) (any, error)
}

func (Arg_Sensor *Sensor) GetValue() (any, error) {
	return Arg_Sensor.__GetValue(Arg_Sensor)
}

func FindSensor(Arg_Sensors []Sensor, Arg_Name string) int {
	var Func_FoundSensorIndex int = -1
	if len(Arg_Name) != 0 {
		for Loop_SensorIndex := range Arg_Sensors {
			if Arg_Sensors[Loop_SensorIndex].Name == Arg_Name {
				Func_FoundSensorIndex = Loop_SensorIndex
				break
			}
		}
	}
	return Func_FoundSensorIndex
}

type SensorDataNV struct {
	GPUIndex    int
	SensorType  int
	GPUFanIndex int
}

const (
	ENUM_NV_SENSOR_TYPE_VRAM                = iota
	ENUM_NV_SENSOR_TYPE_GPU_UTILIZATION     = iota
	ENUM_NV_SENSOR_TYPE_MEMORY_UTILIZATION  = iota
	ENUM_NV_SENSOR_TYPE_ENCODER_UTILIZATION = iota
	ENUM_NV_SENSOR_TYPE_DECODER_UTILIZATION = iota
	ENUM_NV_SENSOR_TYPE_FAN_DUTY            = iota
	ENUM_NV_SENSOR_TYPE_TEMP                = iota
)

const ERROR_NVAPI_FAILED string = "|| The NVIDIA API operation failed!"
const ERROR_INVALID_ARRAY_INDEX = "The index is out of range!"

const PRODUCT_VERSION = "0.1.0"

var RegisteredSensors []Sensor
var ExitRequested atomic.Bool

func NVSensor_GetValue(Arg_Sensor *Sensor) (Func_ResultValue any, Func_ResultError error) {
	Func_SensorData := Arg_Sensor.Data.(SensorDataNV)
	if Func_SensorData.GPUIndex < 0 {
		Func_ResultError = errors.New(ERROR_INVALID_ARRAY_INDEX)
		return
	}
	Func_Device, Func_NVStatus := nvml.DeviceGetHandleByIndex(Func_SensorData.GPUIndex)
	if Func_NVStatus != 0 {
		Func_ResultError = errors.New(ERROR_NVAPI_FAILED)
		return
	}
	switch Func_SensorData.SensorType {
	case ENUM_NV_SENSOR_TYPE_VRAM:
		Func_DeviceMemory, Func_NVStatus := nvml.DeviceGetMemoryInfo(Func_Device)
		if Func_NVStatus != 0 {
			Func_ResultError = errors.New(ERROR_NVAPI_FAILED)
			break
		}
		Func_ResultValue = int(Func_DeviceMemory.Used / 1024 / 1024)
	case ENUM_NV_SENSOR_TYPE_GPU_UTILIZATION:
		fallthrough
	case ENUM_NV_SENSOR_TYPE_MEMORY_UTILIZATION:
		Func_DeviceUtilization, Func_NVStatus := nvml.DeviceGetUtilizationRates(Func_Device)
		if Func_NVStatus != 0 {
			Func_ResultError = errors.New(ERROR_NVAPI_FAILED)
			break
		}
		Func_ResultValue = Func_DeviceUtilization.Gpu
		if Func_SensorData.SensorType == ENUM_NV_SENSOR_TYPE_MEMORY_UTILIZATION {
			Func_ResultValue = Func_DeviceUtilization.Memory
		}
	case ENUM_NV_SENSOR_TYPE_ENCODER_UTILIZATION:
		Func_DeviceUtilization, _, Func_NVStatus := nvml.DeviceGetEncoderUtilization(Func_Device)
		if Func_NVStatus != 0 {
			Func_ResultError = errors.New(ERROR_NVAPI_FAILED)
			break
		}
		Func_ResultValue = Func_DeviceUtilization
	case ENUM_NV_SENSOR_TYPE_DECODER_UTILIZATION:
		Func_DeviceUtilization, _, Func_NVStatus := nvml.DeviceGetDecoderUtilization(Func_Device)
		if Func_NVStatus != 0 {
			Func_ResultError = errors.New(ERROR_NVAPI_FAILED)
			break
		}
		Func_ResultValue = Func_DeviceUtilization
	case ENUM_NV_SENSOR_TYPE_TEMP:
		Func_DeviceTemperature, Func_NVStatus := nvml.DeviceGetTemperature(Func_Device, nvml.TEMPERATURE_GPU)
		if Func_NVStatus != 0 {
			Func_ResultError = errors.New(ERROR_NVAPI_FAILED)
			break
		}
		Func_ResultValue = Func_DeviceTemperature
	case ENUM_NV_SENSOR_TYPE_FAN_DUTY:
		if Func_SensorData.GPUFanIndex < 0 {
			Func_ResultError = errors.New(ERROR_INVALID_ARRAY_INDEX)
			break
		}
		Func_DeviceFanUtilization, Func_NVStatus := nvml.DeviceGetFanSpeed_v2(Func_Device, Func_SensorData.GPUFanIndex)
		if Func_NVStatus != 0 {
			Func_ResultError = errors.New(ERROR_NVAPI_FAILED)
			break
		}
		Func_ResultValue = Func_DeviceFanUtilization
	}
	return
}

func RefreshSensors() *errors.Error {
	Func_RegisteredSensors := make([]Sensor, 0, 8)
	Func_DeviceCount, Func_NVStatus := nvml.DeviceGetCount()
	if Func_NVStatus != 0 {
		return errors.New(ERROR_NVAPI_FAILED)
	}
	for Loop_DeviceIndex := 0; Loop_DeviceIndex < Func_DeviceCount; Loop_DeviceIndex++ {
		Loop_Device, Func_NVStatus := nvml.DeviceGetHandleByIndex(Loop_DeviceIndex)
		if Func_NVStatus != 0 {
			return errors.New(ERROR_NVAPI_FAILED)
		}
		{ // Report VRAM sensor
			Func_DeviceMemory, Func_NVStatus := nvml.DeviceGetMemoryInfo(Loop_Device)
			if Func_NVStatus == 0 {
				Func_RegisteredSensors = append(Func_RegisteredSensors, Sensor{
					Name:         fmt.Sprintf("gpu%v_vram", Loop_DeviceIndex),
					FriendlyName: "Video Memory",
					ValueUnit:    "Mb",
					ValueRange:   ValueRange[int]{0, int(Func_DeviceMemory.Total / 1024 / 1024)},
					Data:         SensorDataNV{Loop_DeviceIndex, ENUM_NV_SENSOR_TYPE_VRAM, -1},
					__GetValue:   NVSensor_GetValue,
				})
			}
		}
		{ // GPU Utilization sensor
			_, Func_NVStatus := nvml.DeviceGetUtilizationRates(Loop_Device)
			if Func_NVStatus == 0 {
				Func_RegisteredSensors = append(Func_RegisteredSensors, Sensor{
					Name:         fmt.Sprintf("gpu%v_utilization", Loop_DeviceIndex),
					FriendlyName: "GPU Utilization",
					ValueUnit:    "%",
					ValueRange:   ValueRange[int]{0, 100},
					Data:         SensorDataNV{Loop_DeviceIndex, ENUM_NV_SENSOR_TYPE_GPU_UTILIZATION, -1},
					__GetValue:   NVSensor_GetValue,
				}, Sensor{
					Name:         fmt.Sprintf("gpu%v_mem_utilization", Loop_DeviceIndex),
					FriendlyName: "GPU Memory Utilization",
					ValueUnit:    "%",
					ValueRange:   ValueRange[int]{0, 100},
					Data:         SensorDataNV{Loop_DeviceIndex, ENUM_NV_SENSOR_TYPE_MEMORY_UTILIZATION, -1},
					__GetValue:   NVSensor_GetValue,
				})
			}
		}
		{ // GPU Encoder utilization
			_, _, Func_NVStatus := nvml.DeviceGetEncoderUtilization(Loop_Device)
			if Func_NVStatus == 0 {
				Func_RegisteredSensors = append(Func_RegisteredSensors, Sensor{
					Name:         fmt.Sprintf("gpu%v_enc_utilization", Loop_DeviceIndex),
					FriendlyName: "GPU Encoder Utilization",
					ValueUnit:    "%",
					ValueRange:   ValueRange[int]{0, 100},
					Data:         SensorDataNV{Loop_DeviceIndex, ENUM_NV_SENSOR_TYPE_ENCODER_UTILIZATION, -1},
					__GetValue:   NVSensor_GetValue,
				})
			}
		}
		{ // GPU Decoder utilization
			_, _, Func_NVStatus := nvml.DeviceGetDecoderUtilization(Loop_Device)
			if Func_NVStatus == 0 {
				Func_RegisteredSensors = append(Func_RegisteredSensors, Sensor{
					Name:         fmt.Sprintf("gpu%v_dec_utilization", Loop_DeviceIndex),
					FriendlyName: "GPU Decoder Utilization",
					ValueUnit:    "%",
					ValueRange:   ValueRange[int]{0, 100},
					Data:         SensorDataNV{Loop_DeviceIndex, ENUM_NV_SENSOR_TYPE_DECODER_UTILIZATION, -1},
					__GetValue:   NVSensor_GetValue,
				})
			}
		}
		{ // GPU Fan duty
			Func_GPUFanCount, Func_NVStatus := nvml.DeviceGetNumFans(Loop_Device)
			if Func_NVStatus != 0 {
				Func_GPUFanCount = 0
			}
			for Loop_GPUFanIndex := Func_GPUFanCount - -1; Loop_GPUFanIndex > -1; Loop_GPUFanIndex-- {
				_, Func_NVStatus := nvml.DeviceGetFanSpeed_v2(Loop_Device, Loop_GPUFanIndex)
				if Func_NVStatus == 0 {
					Func_RegisteredSensors = append(Func_RegisteredSensors, Sensor{
						Name:         fmt.Sprintf("gpu%v_fan%v_duty", Loop_DeviceIndex, Loop_GPUFanIndex),
						FriendlyName: "GPU Fan",
						ValueUnit:    "%",
						ValueRange:   ValueRange[int]{0, 100},
						Data:         SensorDataNV{Loop_DeviceIndex, ENUM_NV_SENSOR_TYPE_FAN_DUTY, Loop_GPUFanIndex},
						__GetValue:   NVSensor_GetValue,
					})
				}
			}
		}
		{ // GPU Temperature
			_, Func_NVStatus := nvml.DeviceGetTemperature(Loop_Device, nvml.TEMPERATURE_GPU)
			if Func_NVStatus == 0 {
				Func_RegisteredSensors = append(Func_RegisteredSensors, Sensor{
					Name:         fmt.Sprintf("gpu%v_temperature", Loop_DeviceIndex),
					FriendlyName: "GPU Temperature",
					ValueUnit:    "°C",
					ValueRange:   ValueRange[int]{0, 100},
					Data:         SensorDataNV{Loop_DeviceIndex, ENUM_NV_SENSOR_TYPE_TEMP, -1},
					__GetValue:   NVSensor_GetValue,
				})
			}
		}
	}
	RegisteredSensors = Func_RegisteredSensors
	return nil
}

func QueryKSGSensorNames(Arg_Sensors []Sensor) []string {
	var Func_KSGSensorNames []string = []string{}
	if len(Arg_Sensors) != 0 {
		Func_KSGSensorNames = make([]string, 0, len(Arg_Sensors))
		for Loop_SensorIndex := range Arg_Sensors {
			Loop_Sensor := &Arg_Sensors[Loop_SensorIndex]
			switch Loop_Sensor.ValueRange.(type) {
			case ValueRange[int]:
				Func_KSGSensorNames = append(Func_KSGSensorNames, Loop_Sensor.Name+"\tinteger\n")
			case ValueRange[int32]:
				Func_KSGSensorNames = append(Func_KSGSensorNames, Loop_Sensor.Name+"\tinteger\n")
			case ValueRange[int64]:
				Func_KSGSensorNames = append(Func_KSGSensorNames, Loop_Sensor.Name+"\tinteger\n")
			case ValueRange[uint]:
				Func_KSGSensorNames = append(Func_KSGSensorNames, Loop_Sensor.Name+"\tinteger\n")
			case ValueRange[uint32]:
				Func_KSGSensorNames = append(Func_KSGSensorNames, Loop_Sensor.Name+"\tinteger\n")
			case ValueRange[uint64]:
				Func_KSGSensorNames = append(Func_KSGSensorNames, Loop_Sensor.Name+"\tinteger\n")
			case ValueRange[float32]:
				Func_KSGSensorNames = append(Func_KSGSensorNames, Loop_Sensor.Name+"\tfloat\n")
			case ValueRange[float64]:
				Func_KSGSensorNames = append(Func_KSGSensorNames, Loop_Sensor.Name+"\tfloat\n")
			}
		}
	}
	return Func_KSGSensorNames
}

func run() error {

	// Initialize NVML
	Func_NVStatus := nvml.Init()
	if Func_NVStatus != 0 {
		return errors.New(ERROR_NVAPI_FAILED)
	}
	defer nvml.Shutdown()

	// Retrieve available GPU sensors
	if Func_Error := RefreshSensors(); Func_Error != nil {
		return Func_Error
	}

	go func() { // Handle interrupt and terminate signals gracefully, letting the app perform cleanup
		Func_SignalChannel := make(chan os.Signal, 1)
		signal.Notify(Func_SignalChannel, syscall.SIGINT, syscall.SIGTERM)
		Func_TriggeredSignal := <-Func_SignalChannel
		if Func_TriggeredSignal == syscall.SIGINT || Func_TriggeredSignal == syscall.SIGTERM {
			ExitRequested.Store(true)
		}
	}()

	// Create the stdin poll
	Func_StdInPoll, Func_Error := syscall.EpollCreate1(0)
	if Func_Error != nil {
		return Func_Error
	}
	defer syscall.Close(Func_StdInPoll)

	// Configure the stdin polling
	Func_Error = syscall.EpollCtl(Func_StdInPoll, syscall.EPOLL_CTL_ADD, syscall.Stdin, &syscall.EpollEvent{
		Events: syscall.EPOLLIN,
	})
	if Func_Error != nil {
		return Func_Error
	}

	// The main logic loop
	fmt.Println("ksysguardd 1.2.0")
	fmt.Print("ksysguardd> ")
	var Func_StdInPolledEvents []syscall.EpollEvent = make([]syscall.EpollEvent, 1)
	var Func_Input string
	for {
		if ExitRequested.Load() { // Exit the app gracefully
			break
		}
		Loop_StdInPollAvailable, _ := syscall.EpollWait(Func_StdInPoll, Func_StdInPolledEvents, 2000) // Wait a little for data to read from stdin
		if Loop_StdInPollAvailable > 0 && Func_StdInPolledEvents[0].Events == syscall.EPOLLIN {
			Loop_ReadInputSize, Func_Error := fmt.Scanln(&Func_Input) // Read available data and process it
			if Func_Error != nil {
				return Func_Error
			}
			if Loop_ReadInputSize > 0 {
				switch Func_Input {
				case "quit":
					ExitRequested.Store(true)
					continue
				case "version":
					fmt.Println(PRODUCT_VERSION)
				case "monitors": // FORMAT: sensor name TAB the sensor type (integer/float) NEWLINE.
					Func_KSGSensorNames := QueryKSGSensorNames(RegisteredSensors)
					for _, Loop_KSGSensorName := range Func_KSGSensorNames {
						fmt.Print(Loop_KSGSensorName)
					}
				default:
					if !strings.HasSuffix(Func_Input, "?") { // FORMAT: value NEWLINE
						var Func_InputSensorIndex int = FindSensor(RegisteredSensors, Func_Input)
						if Func_InputSensorIndex != -1 {
							Func_Value, Func_Error := RegisteredSensors[Func_InputSensorIndex].GetValue()
							if Func_Error == nil {
								fmt.Println(Func_Value)
							}
						}
					} else { // FORMAT: Display Name of the sensor TAB minimum value TAB maximum value TAB units NEWLINE
						var Func_InputSensorIndex int = FindSensor(RegisteredSensors, strings.TrimSuffix(Func_Input, "?"))
						if Func_InputSensorIndex != -1 {
							Func_InputSensor := &RegisteredSensors[Func_InputSensorIndex]
							var Func_InputResponse string
							switch Func_InputSensor.ValueRange.(type) {
							case ValueRange[int]:
								Func_InputSensorRange := Func_InputSensor.ValueRange.(ValueRange[int])
								Func_InputResponse = fmt.Sprintf("%v\t%v\t%v\t%v\n", Func_InputSensor.FriendlyName, Func_InputSensorRange.MinimumValue, Func_InputSensorRange.MaximumValue, Func_InputSensor.ValueUnit)
							case ValueRange[int32]:
								Func_InputSensorRange := Func_InputSensor.ValueRange.(ValueRange[int32])
								Func_InputResponse = fmt.Sprintf("%v\t%v\t%v\t%v\n", Func_InputSensor.FriendlyName, Func_InputSensorRange.MinimumValue, Func_InputSensorRange.MaximumValue, Func_InputSensor.ValueUnit)
							case ValueRange[int64]:
								Func_InputSensorRange := Func_InputSensor.ValueRange.(ValueRange[int64])
								Func_InputResponse = fmt.Sprintf("%v\t%v\t%v\t%v\n", Func_InputSensor.FriendlyName, Func_InputSensorRange.MinimumValue, Func_InputSensorRange.MaximumValue, Func_InputSensor.ValueUnit)
							case ValueRange[uint]:
								Func_InputSensorRange := Func_InputSensor.ValueRange.(ValueRange[uint])
								Func_InputResponse = fmt.Sprintf("%v\t%v\t%v\t%v\n", Func_InputSensor.FriendlyName, Func_InputSensorRange.MinimumValue, Func_InputSensorRange.MaximumValue, Func_InputSensor.ValueUnit)
							case ValueRange[uint32]:
								Func_InputSensorRange := Func_InputSensor.ValueRange.(ValueRange[uint32])
								Func_InputResponse = fmt.Sprintf("%v\t%v\t%v\t%v\n", Func_InputSensor.FriendlyName, Func_InputSensorRange.MinimumValue, Func_InputSensorRange.MaximumValue, Func_InputSensor.ValueUnit)
							case ValueRange[uint64]:
								Func_InputSensorRange := Func_InputSensor.ValueRange.(ValueRange[uint64])
								Func_InputResponse = fmt.Sprintf("%v\t%v\t%v\t%v\n", Func_InputSensor.FriendlyName, Func_InputSensorRange.MinimumValue, Func_InputSensorRange.MaximumValue, Func_InputSensor.ValueUnit)
							case ValueRange[float32]:
								Func_InputSensorRange := Func_InputSensor.ValueRange.(ValueRange[float32])
								Func_InputResponse = fmt.Sprintf("%v\t%v\t%v\t%v\n", Func_InputSensor.FriendlyName, Func_InputSensorRange.MinimumValue, Func_InputSensorRange.MaximumValue, Func_InputSensor.ValueUnit)
							case ValueRange[float64]:
								Func_InputSensorRange := Func_InputSensor.ValueRange.(ValueRange[float64])
								Func_InputResponse = fmt.Sprintf("%v\t%v\t%v\t%v\n", Func_InputSensor.FriendlyName, Func_InputSensorRange.MinimumValue, Func_InputSensorRange.MaximumValue, Func_InputSensor.ValueUnit)
							}
							if len(Func_InputResponse) != 0 {
								fmt.Print(Func_InputResponse)
							}
						}
					}
				}
				fmt.Print("ksysguardd> ")
			}
		}
	}
	return nil
}

func main() {
	Func_ExitError := run()
	if Func_ExitError != nil {
		Func_FancyError, Func_IsFancyError := Func_ExitError.(*errors.Error)
		if Func_IsFancyError {
			fmt.Fprint(os.Stderr, Func_FancyError.ErrorStack())
		} else {
			fmt.Fprint(os.Stderr, Func_ExitError)
		}
		os.Exit(1)
	}
}
