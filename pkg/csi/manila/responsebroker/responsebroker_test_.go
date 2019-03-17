/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package responsebroker

import (
	"fmt"
	"log"
	"sync"
	"testing"
	"time"
)

func TestResponseBroker(t *testing.T) {
	const N = 1000

	var (
		wg sync.WaitGroup
		rb = New()
	)

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			log.Printf("queuing %d", i)
			testRetryTillFinished(i, &wg, rb)
		}(i)
	}

	wg.Wait()
}

func testRetryTillFinished(id int, wg *sync.WaitGroup, rb *ResponseBroker) {
	identifier := fmt.Sprintf("%d", id)

	for {
		finishedCh := make(chan bool)
		log.Printf("launching %d", id)
		go func() {
			handle, isOwner := rb.Acquire(identifier)
			if !isOwner {
				if _, respErr := handle.Read(); respErr == nil {
					handle.Release()
					finishedCh <- true
					return
				}
			}

			log.Printf("running %d", id)

			time.Sleep(5 * time.Second)
			handle.Write(nil, nil)

			rb.Done(identifier)
		}()

		select {
		case <-finishedCh:
			log.Printf("%d finished", id)
			wg.Done()
			return
		case <-time.After(1100 * time.Millisecond):
		}
	}
}
