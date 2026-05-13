module replay_state

open util/ordering[Step]

abstract sig ReplayState {}
one sig Live, ReplayPendingEnd, ReplayDraining extends ReplayState {}

abstract sig Event {}
one sig ReplayStart, ReplayBinary, ReplayEnd, WriteComplete, Reconnect extends Event {}

sig Step {
  state: one ReplayState,
  event: lone Event,
  replayActive: one Bool,
  replayEnded: one Bool,
  disableStdin: one Bool,
  replayWriteDepth: one Int
}

fact InitialState {
  first.state = Live
  no first.event
  first.replayActive = False
  first.replayEnded = True
  first.disableStdin = False
  first.replayWriteDepth = 0
}

fact StateEncoding {
  all s: Step | {
    s.state = Live implies {
      s.replayActive = False
      s.replayEnded = True
      s.disableStdin = False
      s.replayWriteDepth = 0
    }

    s.state = ReplayPendingEnd implies {
      s.replayActive = True
      s.replayEnded = False
      s.disableStdin = True
      s.replayWriteDepth >= 0
    }

    s.state = ReplayDraining implies {
      s.replayActive = False
      s.replayEnded = True
      s.disableStdin = True
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
}

fact Trace {
  all s: Step - first | transition[prev[s], s]
}

assert LiveAlwaysEnablesInput {
  all s: Step | s.state = Live implies s.disableStdin = False
}

assert ReplayStatesAlwaysSuppressInput {
  all s: Step | s.state in ReplayPendingEnd + ReplayDraining implies s.disableStdin = True
}

assert DrainingRequiresQueuedWrites {
  all s: Step | s.state = ReplayDraining implies s.replayWriteDepth > 0
}

assert ReconnectAlwaysResetsToLive {
  all s: Step - first |
    s.event = Reconnect implies {
      s.state = Live
      s.replayActive = False
      s.replayEnded = True
      s.disableStdin = False
      s.replayWriteDepth = 0
    }
}

assert NoStaleSuppressionInLive {
  all s: Step |
    s.state = Live implies {
      s.replayActive = False
      s.disableStdin = False
      s.replayWriteDepth = 0
    }
}

check LiveAlwaysEnablesInput for 8 Step, 8 Int
check ReplayStatesAlwaysSuppressInput for 8 Step, 8 Int
check DrainingRequiresQueuedWrites for 8 Step, 8 Int
check ReconnectAlwaysResetsToLive for 8 Step, 8 Int
check NoStaleSuppressionInLive for 8 Step, 8 Int

