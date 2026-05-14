module replay_state

open util/ordering[Step]

abstract sig ReplayState {}
one sig Live, ReplayPendingEnd, ReplayDraining extends ReplayState {}

abstract sig Event {}
one sig ReplayStart, ReplayBinary, ReplayEnd, WriteComplete, SocketClose, ReplayEndWriteFail, Reconnect extends Event {}

abstract sig Flag {}
one sig Enabled, Disabled extends Flag {}

sig Step {
  state: one ReplayState,
  event: lone Event,
  replayActive: one Flag,
  awaitingReplayEnd: one Flag,
  disableStdin: one Flag,
  replayWriteDepth: one Int
}

fact InitialState {
  first.state = Live
  no first.event
  first.replayActive = Disabled
  first.awaitingReplayEnd = Disabled
  first.disableStdin = Disabled
  first.replayWriteDepth = 0
}

fact StateEncoding {
  all s: Step | {
    s.state = Live implies {
      s.replayActive = Disabled
      s.awaitingReplayEnd = Disabled
      s.disableStdin = Disabled
      s.replayWriteDepth = 0
    }

    s.state = ReplayPendingEnd implies {
      s.replayActive = Enabled
      s.awaitingReplayEnd = Enabled
      s.disableStdin = Enabled
      s.replayWriteDepth >= 0
    }

    s.state = ReplayDraining implies {
      s.replayActive = Disabled
      s.awaitingReplayEnd = Disabled
      s.disableStdin = Enabled
      s.replayWriteDepth > 0
    }
  }
}

pred transition[cur, next: Step] {
  next.event = ReplayStart implies {
    cur.state = Live
    next.state = ReplayPendingEnd
    next.replayWriteDepth = 0
  }

  next.event = ReplayBinary implies {
    cur.state = ReplayPendingEnd
    next.state = ReplayPendingEnd
    next.replayWriteDepth = cur.replayWriteDepth.plus[1]
  }

  next.event = ReplayEnd implies {
    cur.state = ReplayPendingEnd
    cur.replayWriteDepth = 0 implies {
      next.state = Live
      next.replayWriteDepth = 0
    }
    cur.replayWriteDepth > 0 implies {
      next.state = ReplayDraining
      next.replayWriteDepth = cur.replayWriteDepth
    }
  }

  next.event = WriteComplete implies {
    cur.state in ReplayPendingEnd + ReplayDraining

    cur.state = ReplayPendingEnd implies {
      cur.replayWriteDepth > 0
      next.state = ReplayPendingEnd
      next.replayWriteDepth = cur.replayWriteDepth.minus[1]
    }

    cur.state = ReplayDraining implies {
      cur.replayWriteDepth > 0
      cur.replayWriteDepth = 1 implies {
        next.state = Live
        next.replayWriteDepth = 0
      }
      cur.replayWriteDepth > 1 implies {
        next.state = ReplayDraining
        next.replayWriteDepth = cur.replayWriteDepth.minus[1]
      }
    }
  }

  next.event = Reconnect implies {
    next.state = Live
    next.replayWriteDepth = 0
  }

  next.event = SocketClose implies {
    next.state = cur.state
    next.replayActive = cur.replayActive
    next.awaitingReplayEnd = cur.awaitingReplayEnd
    next.disableStdin = cur.disableStdin
    next.replayWriteDepth = cur.replayWriteDepth
  }

  next.event = ReplayEndWriteFail implies {
    cur.state = ReplayPendingEnd
    next.state = ReplayPendingEnd
    next.replayActive = Enabled
    next.awaitingReplayEnd = Enabled
    next.disableStdin = Enabled
    next.replayWriteDepth = cur.replayWriteDepth
  }
}

fact Trace {
  all s: Step - first | transition[prev[s], s]
}

assert LiveAlwaysEnablesInput {
  all s: Step | s.state = Live implies s.disableStdin = Disabled
}

assert ReplayStatesAlwaysSuppressInput {
  all s: Step | s.state in ReplayPendingEnd + ReplayDraining implies s.disableStdin = Enabled
}

assert DrainingRequiresQueuedWrites {
  all s: Step | s.state = ReplayDraining implies s.replayWriteDepth > 0
}

assert ReconnectAlwaysResetsToLive {
  all s: Step - first |
    s.event = Reconnect implies {
      s.state = Live
      s.replayActive = Disabled
      s.awaitingReplayEnd = Disabled
      s.disableStdin = Disabled
      s.replayWriteDepth = 0
    }
}

assert NoStaleSuppressionInLive {
  all s: Step |
    s.state = Live implies {
      s.replayActive = Disabled
      s.disableStdin = Disabled
      s.replayWriteDepth = 0
    }
}

assert ReplayEndWriteFailureLeavesReplayPendingUntilReconnect {
  all s: Step - first |
    s.event = ReplayEndWriteFail implies {
      s.state = ReplayPendingEnd
      s.replayActive = Enabled
      s.awaitingReplayEnd = Enabled
      s.disableStdin = Enabled
    }
}

assert SocketCloseDoesNotFalselyRestoreLive {
  all s: Step - first |
    s.event = SocketClose and prev[s].state in ReplayPendingEnd + ReplayDraining implies {
      s.state = prev[s].state
      s.disableStdin = Enabled
    }
}

check LiveAlwaysEnablesInput for 8 Step, 8 Int
check ReplayStatesAlwaysSuppressInput for 8 Step, 8 Int
check DrainingRequiresQueuedWrites for 8 Step, 8 Int
check ReconnectAlwaysResetsToLive for 8 Step, 8 Int
check NoStaleSuppressionInLive for 8 Step, 8 Int
check ReplayEndWriteFailureLeavesReplayPendingUntilReconnect for 8 Step, 8 Int
check SocketCloseDoesNotFalselyRestoreLive for 8 Step, 8 Int
