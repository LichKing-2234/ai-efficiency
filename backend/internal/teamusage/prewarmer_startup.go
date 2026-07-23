package teamusage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type startupLanePlan struct {
	identity PrewarmCacheIdentity
	manifest PrewarmManifest
	missing  []PrewarmSegmentClass
	leased   map[PrewarmSegmentClass]leasedPrewarmReference
	failures []error
}

type startupSegmentTask struct {
	laneIndex int
	class     PrewarmSegmentClass
}

type startupSegmentResult struct {
	task   startupSegmentTask
	leased leasedPrewarmReference
	err    error
}

const startupSegmentWorkerCount = prewarmSourceSlotCount

func startupRefreshClass(class PrewarmSegmentClass) string {
	if class == SegmentTodayHour {
		return prewarmMovingRefreshClass
	}
	return string(class)
}

func startupTaskCount(plans []startupLanePlan) int {
	count := 0
	for _, plan := range plans {
		count += len(plan.missing)
	}
	return count
}

func sharedCurrentNeeded(plans []startupLanePlan) bool {
	for _, plan := range plans {
		if plan.manifest.CurrentStats.Key == "" {
			return true
		}
	}
	return false
}

func applyStartupCurrentReference(plans []startupLanePlan, ref PrewarmValueReference) {
	for index := range plans {
		if plans[index].manifest.CurrentStats.Key == "" {
			plans[index].manifest.CurrentStats = ref
		}
	}
}

func applyStartupSegmentReferences(
	manifest *PrewarmManifest,
	leased map[PrewarmSegmentClass]leasedPrewarmReference,
) {
	if manifest == nil {
		return
	}
	if value, ok := leased[SegmentHistory29d]; ok {
		manifest.History29d = value.reference
	}
	if value, ok := leased[SegmentHistory6d]; ok {
		manifest.History6d = value.reference
	}
	if value, ok := leased[SegmentTodayHour]; ok {
		manifest.TodayHour = value.reference
	}
}

func startupSegmentClaims(leased map[PrewarmSegmentClass]leasedPrewarmReference) []PrewarmLeaseClaim {
	claims := make([]PrewarmLeaseClaim, 0, len(leased))
	for _, class := range []PrewarmSegmentClass{SegmentHistory29d, SegmentHistory6d, SegmentTodayHour} {
		value, ok := leased[class]
		if !ok {
			continue
		}
		claims = append(claims, PrewarmLeaseClaim{Key: value.leaseKey, Token: value.token})
	}
	return claims
}

func (p *Prewarmer) planStartupLanes(
	ctx context.Context,
	binding ProviderBinding,
	batchTime time.Time,
) ([]startupLanePlan, []error) {
	plans := make([]startupLanePlan, 0, len(p.timezones))
	var failures []error
	for _, timezone := range p.timezones {
		anchorDate, err := prewarmLocalAnchorDate(timezone, batchTime)
		if err != nil {
			failures = append(failures, newPrewarmLifecycleFailure(
				PrewarmCycleStartup, timezone, false, fmt.Errorf("startup anchor: %w", err),
			))
			continue
		}
		safe, err := SplitSafe(timezone, anchorDate)
		if err != nil {
			failures = append(failures, newPrewarmLifecycleFailure(
				PrewarmCycleStartup, timezone, false, fmt.Errorf("startup split safety: %w", err),
			))
			continue
		}
		if !safe {
			continue
		}
		identity := PrewarmCacheIdentity{
			ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion,
			Timezone: timezone, AnchorDate: anchorDate,
		}
		if err := p.requireCoordinatorOwned(ctx); err != nil {
			failures = append(failures, newPrewarmLifecycleFailure(PrewarmCycleStartup, timezone, false, err))
			continue
		}
		previous, ok, err := p.cache.Read(ctx, identity)
		if err != nil {
			failures = append(failures, newPrewarmLifecycleFailure(
				PrewarmCycleStartup, timezone, false, fmt.Errorf("startup read: %w", err),
			))
			continue
		}
		if !startupNeedsRecovery(previous, ok) {
			continue
		}
		manifest := newPrewarmManifestCandidate(identity, previous, batchTime)
		if !ok || previous.CurrentStatsStatus == PrewarmValueMissing || previous.CurrentStatsStatus == PrewarmValueHardExpired {
			manifest.CurrentStats = PrewarmValueReference{}
		}
		plans = append(plans, startupLanePlan{
			identity: identity,
			manifest: manifest,
			missing:  startupMissingSegmentClasses(previous, ok),
			leased:   make(map[PrewarmSegmentClass]leasedPrewarmReference, 3),
		})
	}
	return plans, failures
}

func (p *Prewarmer) fetchStartupSegments(
	ctx context.Context,
	binding ProviderBinding,
	plans []startupLanePlan,
) []error {
	tasks := make(chan startupSegmentTask)
	results := make(chan startupSegmentResult, startupTaskCount(plans))
	var workers sync.WaitGroup
	for worker := 0; worker < startupSegmentWorkerCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for task := range tasks {
				identity := plans[task.laneIndex].identity
				leased, err := p.fetchLeasedSegment(
					ctx, binding, identity.Timezone, identity.AnchorDate,
					task.class, startupRefreshClass(task.class),
				)
				results <- startupSegmentResult{task: task, leased: leased, err: err}
			}
		}()
	}
	go func() {
		defer close(tasks)
		for laneIndex := range plans {
			for _, class := range plans[laneIndex].missing {
				select {
				case tasks <- startupSegmentTask{laneIndex: laneIndex, class: class}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	var failures []error
	for result := range results {
		plan := &plans[result.task.laneIndex]
		if result.err != nil {
			failure := newPrewarmLifecycleFailure(
				PrewarmCycleStartup, plan.identity.Timezone, false, result.err,
			)
			plan.failures = append(plan.failures, failure)
			failures = append(failures, failure)
			continue
		}
		plan.leased[result.task.class] = result.leased
	}
	return failures
}

func (p *Prewarmer) publishStartupCohort(
	ctx context.Context,
	binding ProviderBinding,
	plans []startupLanePlan,
) []error {
	var failures []error
	for index := range plans {
		plan := &plans[index]
		applyStartupSegmentReferences(&plan.manifest, plan.leased)
		if len(plan.failures) != 0 || !prewarmManifestReferencesPresent(plan.manifest) {
			continue
		}
		if err := errors.Join(ctx.Err(), p.requireCoordinatorOwned(ctx)); err != nil {
			failures = append(failures, newPrewarmLifecycleFailure(
				PrewarmCycleStartup, plan.identity.Timezone, false, err,
			))
			continue
		}
		if err := p.publishIfCurrent(ctx, binding, startupSegmentClaims(plan.leased), plan.manifest); err != nil {
			failures = append(failures, newPrewarmLifecycleFailure(
				PrewarmCycleStartup, plan.identity.Timezone, false, err,
			))
			continue
		}
		p.options.Metrics.SetLastSuccess("startup", plan.identity.Timezone, p.options.Now())
	}
	return failures
}
