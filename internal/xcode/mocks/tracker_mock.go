// Code generated by moq; DO NOT EDIT.
// github.com/matryer/moq

package mocks

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcode"
	"sync"
	"time"
)

// Ensure, that StepAnalyticsTrackerMock does implement xcode.StepAnalyticsTracker.
// If this is not the case, regenerate this file with moq.
var _ xcode.StepAnalyticsTracker = &StepAnalyticsTrackerMock{}

// StepAnalyticsTrackerMock is a mock implementation of xcode.StepAnalyticsTracker.
//
//	func TestSomethingThatUsesStepAnalyticsTracker(t *testing.T) {
//
//		// make and configure a mocked xcode.StepAnalyticsTracker
//		mockedStepAnalyticsTracker := &StepAnalyticsTrackerMock{
//			LogDerivedDataDownloadedFunc: func(duration time.Duration, stats xcode.DownloadFilesStats)  {
//				panic("mock out the LogDerivedDataDownloaded method")
//			},
//			LogDerivedDataUploadedFunc: func(duration time.Duration, stats xcode.UploadFilesStats)  {
//				panic("mock out the LogDerivedDataUploaded method")
//			},
//			LogMetadataLoadedFunc: func(duration time.Duration, metadataKeyType string, totalFileCount int, restoredFileCount int)  {
//				panic("mock out the LogMetadataLoaded method")
//			},
//			LogMetadataSavedFunc: func(duration time.Duration, fileCount int, size int64)  {
//				panic("mock out the LogMetadataSaved method")
//			},
//			LogRestoreFinishedFunc: func(totalDuration time.Duration, err error)  {
//				panic("mock out the LogRestoreFinished method")
//			},
//			LogSaveFinishedFunc: func(totalDuration time.Duration, err error)  {
//				panic("mock out the LogSaveFinished method")
//			},
//			WaitFunc: func()  {
//				panic("mock out the Wait method")
//			},
//		}
//
//		// use mockedStepAnalyticsTracker in code that requires xcode.StepAnalyticsTracker
//		// and then make assertions.
//
//	}
type StepAnalyticsTrackerMock struct {
	// LogDerivedDataDownloadedFunc mocks the LogDerivedDataDownloaded method.
	LogDerivedDataDownloadedFunc func(duration time.Duration, stats xcode.DownloadFilesStats)

	// LogDerivedDataUploadedFunc mocks the LogDerivedDataUploaded method.
	LogDerivedDataUploadedFunc func(duration time.Duration, stats xcode.UploadFilesStats)

	// LogMetadataLoadedFunc mocks the LogMetadataLoaded method.
	LogMetadataLoadedFunc func(duration time.Duration, metadataKeyType string, totalFileCount int, restoredFileCount int)

	// LogMetadataSavedFunc mocks the LogMetadataSaved method.
	LogMetadataSavedFunc func(duration time.Duration, fileCount int, size int64)

	// LogRestoreFinishedFunc mocks the LogRestoreFinished method.
	LogRestoreFinishedFunc func(totalDuration time.Duration, err error)

	// LogSaveFinishedFunc mocks the LogSaveFinished method.
	LogSaveFinishedFunc func(totalDuration time.Duration, err error)

	// WaitFunc mocks the Wait method.
	WaitFunc func()

	// calls tracks calls to the methods.
	calls struct {
		// LogDerivedDataDownloaded holds details about calls to the LogDerivedDataDownloaded method.
		LogDerivedDataDownloaded []struct {
			// Duration is the duration argument value.
			Duration time.Duration
			// Stats is the stats argument value.
			Stats xcode.DownloadFilesStats
		}
		// LogDerivedDataUploaded holds details about calls to the LogDerivedDataUploaded method.
		LogDerivedDataUploaded []struct {
			// Duration is the duration argument value.
			Duration time.Duration
			// Stats is the stats argument value.
			Stats xcode.UploadFilesStats
		}
		// LogMetadataLoaded holds details about calls to the LogMetadataLoaded method.
		LogMetadataLoaded []struct {
			// Duration is the duration argument value.
			Duration time.Duration
			// MetadataKeyType is the metadataKeyType argument value.
			MetadataKeyType string
			// TotalFileCount is the totalFileCount argument value.
			TotalFileCount int
			// RestoredFileCount is the restoredFileCount argument value.
			RestoredFileCount int
		}
		// LogMetadataSaved holds details about calls to the LogMetadataSaved method.
		LogMetadataSaved []struct {
			// Duration is the duration argument value.
			Duration time.Duration
			// FileCount is the fileCount argument value.
			FileCount int
			// Size is the size argument value.
			Size int64
		}
		// LogRestoreFinished holds details about calls to the LogRestoreFinished method.
		LogRestoreFinished []struct {
			// TotalDuration is the totalDuration argument value.
			TotalDuration time.Duration
			// Err is the err argument value.
			Err error
		}
		// LogSaveFinished holds details about calls to the LogSaveFinished method.
		LogSaveFinished []struct {
			// TotalDuration is the totalDuration argument value.
			TotalDuration time.Duration
			// Err is the err argument value.
			Err error
		}
		// Wait holds details about calls to the Wait method.
		Wait []struct {
		}
	}
	lockLogDerivedDataDownloaded sync.RWMutex
	lockLogDerivedDataUploaded   sync.RWMutex
	lockLogMetadataLoaded        sync.RWMutex
	lockLogMetadataSaved         sync.RWMutex
	lockLogRestoreFinished       sync.RWMutex
	lockLogSaveFinished          sync.RWMutex
	lockWait                     sync.RWMutex
}

// LogDerivedDataDownloaded calls LogDerivedDataDownloadedFunc.
func (mock *StepAnalyticsTrackerMock) LogDerivedDataDownloaded(duration time.Duration, stats xcode.DownloadFilesStats) {
	if mock.LogDerivedDataDownloadedFunc == nil {
		panic("StepAnalyticsTrackerMock.LogDerivedDataDownloadedFunc: method is nil but StepAnalyticsTracker.LogDerivedDataDownloaded was just called")
	}
	callInfo := struct {
		Duration time.Duration
		Stats    xcode.DownloadFilesStats
	}{
		Duration: duration,
		Stats:    stats,
	}
	mock.lockLogDerivedDataDownloaded.Lock()
	mock.calls.LogDerivedDataDownloaded = append(mock.calls.LogDerivedDataDownloaded, callInfo)
	mock.lockLogDerivedDataDownloaded.Unlock()
	mock.LogDerivedDataDownloadedFunc(duration, stats)
}

// LogDerivedDataDownloadedCalls gets all the calls that were made to LogDerivedDataDownloaded.
// Check the length with:
//
//	len(mockedStepAnalyticsTracker.LogDerivedDataDownloadedCalls())
func (mock *StepAnalyticsTrackerMock) LogDerivedDataDownloadedCalls() []struct {
	Duration time.Duration
	Stats    xcode.DownloadFilesStats
} {
	var calls []struct {
		Duration time.Duration
		Stats    xcode.DownloadFilesStats
	}
	mock.lockLogDerivedDataDownloaded.RLock()
	calls = mock.calls.LogDerivedDataDownloaded
	mock.lockLogDerivedDataDownloaded.RUnlock()
	return calls
}

// LogDerivedDataUploaded calls LogDerivedDataUploadedFunc.
func (mock *StepAnalyticsTrackerMock) LogDerivedDataUploaded(duration time.Duration, stats xcode.UploadFilesStats) {
	if mock.LogDerivedDataUploadedFunc == nil {
		panic("StepAnalyticsTrackerMock.LogDerivedDataUploadedFunc: method is nil but StepAnalyticsTracker.LogDerivedDataUploaded was just called")
	}
	callInfo := struct {
		Duration time.Duration
		Stats    xcode.UploadFilesStats
	}{
		Duration: duration,
		Stats:    stats,
	}
	mock.lockLogDerivedDataUploaded.Lock()
	mock.calls.LogDerivedDataUploaded = append(mock.calls.LogDerivedDataUploaded, callInfo)
	mock.lockLogDerivedDataUploaded.Unlock()
	mock.LogDerivedDataUploadedFunc(duration, stats)
}

// LogDerivedDataUploadedCalls gets all the calls that were made to LogDerivedDataUploaded.
// Check the length with:
//
//	len(mockedStepAnalyticsTracker.LogDerivedDataUploadedCalls())
func (mock *StepAnalyticsTrackerMock) LogDerivedDataUploadedCalls() []struct {
	Duration time.Duration
	Stats    xcode.UploadFilesStats
} {
	var calls []struct {
		Duration time.Duration
		Stats    xcode.UploadFilesStats
	}
	mock.lockLogDerivedDataUploaded.RLock()
	calls = mock.calls.LogDerivedDataUploaded
	mock.lockLogDerivedDataUploaded.RUnlock()
	return calls
}

// LogMetadataLoaded calls LogMetadataLoadedFunc.
func (mock *StepAnalyticsTrackerMock) LogMetadataLoaded(duration time.Duration, metadataKeyType string, totalFileCount int, restoredFileCount int) {
	if mock.LogMetadataLoadedFunc == nil {
		panic("StepAnalyticsTrackerMock.LogMetadataLoadedFunc: method is nil but StepAnalyticsTracker.LogMetadataLoaded was just called")
	}
	callInfo := struct {
		Duration          time.Duration
		MetadataKeyType   string
		TotalFileCount    int
		RestoredFileCount int
	}{
		Duration:          duration,
		MetadataKeyType:   metadataKeyType,
		TotalFileCount:    totalFileCount,
		RestoredFileCount: restoredFileCount,
	}
	mock.lockLogMetadataLoaded.Lock()
	mock.calls.LogMetadataLoaded = append(mock.calls.LogMetadataLoaded, callInfo)
	mock.lockLogMetadataLoaded.Unlock()
	mock.LogMetadataLoadedFunc(duration, metadataKeyType, totalFileCount, restoredFileCount)
}

// LogMetadataLoadedCalls gets all the calls that were made to LogMetadataLoaded.
// Check the length with:
//
//	len(mockedStepAnalyticsTracker.LogMetadataLoadedCalls())
func (mock *StepAnalyticsTrackerMock) LogMetadataLoadedCalls() []struct {
	Duration          time.Duration
	MetadataKeyType   string
	TotalFileCount    int
	RestoredFileCount int
} {
	var calls []struct {
		Duration          time.Duration
		MetadataKeyType   string
		TotalFileCount    int
		RestoredFileCount int
	}
	mock.lockLogMetadataLoaded.RLock()
	calls = mock.calls.LogMetadataLoaded
	mock.lockLogMetadataLoaded.RUnlock()
	return calls
}

// LogMetadataSaved calls LogMetadataSavedFunc.
func (mock *StepAnalyticsTrackerMock) LogMetadataSaved(duration time.Duration, fileCount int, size int64) {
	if mock.LogMetadataSavedFunc == nil {
		panic("StepAnalyticsTrackerMock.LogMetadataSavedFunc: method is nil but StepAnalyticsTracker.LogMetadataSaved was just called")
	}
	callInfo := struct {
		Duration  time.Duration
		FileCount int
		Size      int64
	}{
		Duration:  duration,
		FileCount: fileCount,
		Size:      size,
	}
	mock.lockLogMetadataSaved.Lock()
	mock.calls.LogMetadataSaved = append(mock.calls.LogMetadataSaved, callInfo)
	mock.lockLogMetadataSaved.Unlock()
	mock.LogMetadataSavedFunc(duration, fileCount, size)
}

// LogMetadataSavedCalls gets all the calls that were made to LogMetadataSaved.
// Check the length with:
//
//	len(mockedStepAnalyticsTracker.LogMetadataSavedCalls())
func (mock *StepAnalyticsTrackerMock) LogMetadataSavedCalls() []struct {
	Duration  time.Duration
	FileCount int
	Size      int64
} {
	var calls []struct {
		Duration  time.Duration
		FileCount int
		Size      int64
	}
	mock.lockLogMetadataSaved.RLock()
	calls = mock.calls.LogMetadataSaved
	mock.lockLogMetadataSaved.RUnlock()
	return calls
}

// LogRestoreFinished calls LogRestoreFinishedFunc.
func (mock *StepAnalyticsTrackerMock) LogRestoreFinished(totalDuration time.Duration, err error) {
	if mock.LogRestoreFinishedFunc == nil {
		panic("StepAnalyticsTrackerMock.LogRestoreFinishedFunc: method is nil but StepAnalyticsTracker.LogRestoreFinished was just called")
	}
	callInfo := struct {
		TotalDuration time.Duration
		Err           error
	}{
		TotalDuration: totalDuration,
		Err:           err,
	}
	mock.lockLogRestoreFinished.Lock()
	mock.calls.LogRestoreFinished = append(mock.calls.LogRestoreFinished, callInfo)
	mock.lockLogRestoreFinished.Unlock()
	mock.LogRestoreFinishedFunc(totalDuration, err)
}

// LogRestoreFinishedCalls gets all the calls that were made to LogRestoreFinished.
// Check the length with:
//
//	len(mockedStepAnalyticsTracker.LogRestoreFinishedCalls())
func (mock *StepAnalyticsTrackerMock) LogRestoreFinishedCalls() []struct {
	TotalDuration time.Duration
	Err           error
} {
	var calls []struct {
		TotalDuration time.Duration
		Err           error
	}
	mock.lockLogRestoreFinished.RLock()
	calls = mock.calls.LogRestoreFinished
	mock.lockLogRestoreFinished.RUnlock()
	return calls
}

// LogSaveFinished calls LogSaveFinishedFunc.
func (mock *StepAnalyticsTrackerMock) LogSaveFinished(totalDuration time.Duration, err error) {
	if mock.LogSaveFinishedFunc == nil {
		panic("StepAnalyticsTrackerMock.LogSaveFinishedFunc: method is nil but StepAnalyticsTracker.LogSaveFinished was just called")
	}
	callInfo := struct {
		TotalDuration time.Duration
		Err           error
	}{
		TotalDuration: totalDuration,
		Err:           err,
	}
	mock.lockLogSaveFinished.Lock()
	mock.calls.LogSaveFinished = append(mock.calls.LogSaveFinished, callInfo)
	mock.lockLogSaveFinished.Unlock()
	mock.LogSaveFinishedFunc(totalDuration, err)
}

// LogSaveFinishedCalls gets all the calls that were made to LogSaveFinished.
// Check the length with:
//
//	len(mockedStepAnalyticsTracker.LogSaveFinishedCalls())
func (mock *StepAnalyticsTrackerMock) LogSaveFinishedCalls() []struct {
	TotalDuration time.Duration
	Err           error
} {
	var calls []struct {
		TotalDuration time.Duration
		Err           error
	}
	mock.lockLogSaveFinished.RLock()
	calls = mock.calls.LogSaveFinished
	mock.lockLogSaveFinished.RUnlock()
	return calls
}

// Wait calls WaitFunc.
func (mock *StepAnalyticsTrackerMock) Wait() {
	if mock.WaitFunc == nil {
		panic("StepAnalyticsTrackerMock.WaitFunc: method is nil but StepAnalyticsTracker.Wait was just called")
	}
	callInfo := struct {
	}{}
	mock.lockWait.Lock()
	mock.calls.Wait = append(mock.calls.Wait, callInfo)
	mock.lockWait.Unlock()
	mock.WaitFunc()
}

// WaitCalls gets all the calls that were made to Wait.
// Check the length with:
//
//	len(mockedStepAnalyticsTracker.WaitCalls())
func (mock *StepAnalyticsTrackerMock) WaitCalls() []struct {
} {
	var calls []struct {
	}
	mock.lockWait.RLock()
	calls = mock.calls.Wait
	mock.lockWait.RUnlock()
	return calls
}