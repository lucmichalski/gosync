package jobs

type Job interface {
	Run() (Job, error)
}

type JobRunner struct {
	JobPool  chan int
	DoneJobs chan Job
	HasLimit bool
}

func NewJobRunner(maxJobs int) *JobRunner {
	if maxJobs < 0 {
		maxJobs = 0
	}
	hasLimit := maxJobs > 0
	jobPool := make(chan int, maxJobs)
	doneJobs := make(chan Job)
	return &JobRunner{
		JobPool:  jobPool,
		DoneJobs: doneJobs,
		HasLimit: hasLimit,
	}
}

func (jr *JobRunner) RunJob(j Job) {
	go func() {
		if jr.HasLimit {
			jr.JobPool <- 1
		}
		defer func() {
			recover()
			if jr.HasLimit {
				<-jr.JobPool
			}
			jr.DoneJobs <- j
		}()
		j, _ = j.Run()
	}()
}

func (jr *JobRunner) Get() Job {
	return <-jr.DoneJobs
}
