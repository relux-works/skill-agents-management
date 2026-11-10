- Add explicit-only unmeasured model claims with dated, attributed availability
  evidence and no numeric rank. Add claude-fable-5-2 for claude-code with
  low/medium/high/xhigh/max, recommended high when explicit effort is omitted.
  Unmeasured models never affect lineup defaults, order or ties.
- Make Model.Rank and benchdata.Model.Rank optional pointers, mutually exclusive
  with Unmeasured. Ranked score validation and existing ranked rows are unchanged;
  consumers reading raw model rows must guard Rank before dereferencing it.
