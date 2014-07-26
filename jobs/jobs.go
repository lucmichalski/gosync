package jobs

type Job interface {
	Run() (Job, error)
}

type JobRunner struct {
	JobPool  chan int
	DoneJobs chan Job
}

func NewJobRunner(maxJobs int) *JobRunner {
	if maxJobs < 0 {
		maxJobs = 0
	}
	jobPool := make(chan int, maxJobs)
	doneJobs := make(chan Job)
	return &JobRunner{
		JobPool:  jobPool,
		DoneJobs: doneJobs,
	}
}

func (jr *JobRunner) RunJob(j Job) {
	go func() {
		jr.JobPool <- 1
		defer func() {
			_ = recover()
			_ = <-jr.JobPool
			jr.DoneJobs <- j
		}()
		j, _ = j.Run()
	}()
}

func (jr *JobRunner) Get() Job {
	return <-jr.DoneJobs
}
