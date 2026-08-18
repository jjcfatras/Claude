### Code review

Inline comment posting failed. All issues listed below.

**modules/eks-managed-node-group/main.tf:325**

🟡 **Medium** (Confidence: 85/100) - identical dynamic block added twice instead of a shared local

<!-- cr-finding id="4f1e101d09c2" snippet64="ICBkeW5hbWljICJuZXR3b3JrX3BlcmZvcm1hbmNlX29wdGlvbnMiIHsKICAgIGZvcl9lYWNoID0gdmFyLm5ldHdvcmtfcGVyZm9ybWFuY2Vfb3B0aW9ucyAhPSBudWxsID8gW3Zhci5uZXR3b3JrX3BlcmZvcm1hbmNlX29wdGlvbnNdIDogW10KCiAgICBjb250ZW50IHsKICAgICAgYmFuZHdpZHRoX3dlaWdodGluZyA9IG5ldHdvcmtfcGVyZm9ybWFuY2Vfb3B0aW9ucy52YWx1ZS5iYW5kd2lkdGhfd2VpZ2h0aW5nCiAgICB9CiAgfQ==" -->

**Issue & impact:** This dynamic block is byte-identical to the one added in modules/self-managed-node-group/main.tf, including the for_each guard. Two copies means a future change to the guard has to be found and applied twice, and the launch-template resources are already the pair that drifted last time.

**Code:**

```hcl
  dynamic "network_performance_options" {
    for_each = var.network_performance_options != null ? [var.network_performance_options] : []

    content {
      bandwidth_weighting = network_performance_options.value.bandwidth_weighting
    }
  }
```

**Suggested fix:**

```hcl
  dynamic "network_performance_options" {
    for_each = local.network_performance_options[*]

    content {
      bandwidth_weighting = network_performance_options.value.bandwidth_weighting
    }
  }
```

**modules/eks-managed-node-group/variables.tf:262**

🟡 **Medium** (Confidence: 80/100) - new optional attribute accepts any string for bandwidth_weighting

<!-- cr-finding id="55e8acb69526" snippet64="dmFyaWFibGUgIm5ldHdvcmtfcGVyZm9ybWFuY2Vfb3B0aW9ucyIgewogIGRlc2NyaXB0aW9uID0gIlRoZSBuZXR3b3JrIHBlcmZvcm1hbmNlIG9wdGlvbnMgZm9yIHRoZSBpbnN0YW5jZSIKICB0eXBlID0gb2JqZWN0KHsKICAgIGJhbmR3aWR0aF93ZWlnaHRpbmcgPSBvcHRpb25hbChzdHJpbmcpCiAgfSkKICBkZWZhdWx0ID0gbnVsbAp9" -->

**Issue & impact:** The declared object type constrains bandwidth_weighting to a string but not to the values EC2 accepts, so a typo surfaces as an API error during apply rather than at plan time. A validation block catches it before any infrastructure is touched.

**Code:**

```hcl
variable "network_performance_options" {
  description = "The network performance options for the instance"
  type = object({
    bandwidth_weighting = optional(string)
  })
  default = null
}
```

**Suggested fix:**

```hcl
variable "network_performance_options" {
  description = "The network performance options for the instance"
  type = object({
    bandwidth_weighting = optional(string)
  })
  default = null

  validation {
    condition     = try(contains(["default", "vpc-1", "ebs-1"], var.network_performance_options.bandwidth_weighting), true)
    error_message = "bandwidth_weighting must be one of: default, vpc-1, ebs-1."
  }
}
```

**modules/self-managed-node-group/variables.tf:234**

🟡 **Medium** (Confidence: 80/100) - null default makes the nested attribute unreachable without a guard

<!-- cr-finding id="9545cdee25c2" snippet64="dmFyaWFibGUgIm5ldHdvcmtfcGVyZm9ybWFuY2Vfb3B0aW9ucyIgewogIGRlc2NyaXB0aW9uID0gIlRoZSBuZXR3b3JrIHBlcmZvcm1hbmNlIG9wdGlvbnMgZm9yIHRoZSBpbnN0YW5jZSIKICB0eXBlID0gb2JqZWN0KHsKICAgIGJhbmR3aWR0aF93ZWlnaHRpbmcgPSBvcHRpb25hbChzdHJpbmcpCiAgfSkKICBkZWZhdWx0ID0gbnVsbAp9" -->

**Issue & impact:** Because the whole object defaults to null, any expression reading the nested attribute directly errors out instead of yielding null. Every consumer therefore needs its own presence check before dereferencing.

**Code:**

```hcl
variable "network_performance_options" {
  description = "The network performance options for the instance"
  type = object({
    bandwidth_weighting = optional(string)
  })
  default = null
}
```

**node_groups.tf:362**

🟡 **Medium** (Confidence: 80/100) - pass-through wiring duplicated across both module invocations

<!-- cr-finding id="6b3a1e3a903d" snippet64="ICBuZXR3b3JrX3BlcmZvcm1hbmNlX29wdGlvbnMgICAgICAgID0gZWFjaC52YWx1ZS5uZXR3b3JrX3BlcmZvcm1hbmNlX29wdGlvbnM=" -->

**Issue & impact:** Adding one attribute here requires the identical line in the second module call further down, and the two node-group modules now expose an identical block that must be kept in lockstep by hand. A shared local would let both call sites read from one place.

**Code:**

```hcl
  network_performance_options        = each.value.network_performance_options
```

**node_groups.tf:509**

📝 **Minor** (Confidence: 80/100) - each.value lookup repeated per attribute in a wide for_each

<!-- cr-finding id="7f6c8abbefdb" snippet64="ICBuZXR3b3JrX3BlcmZvcm1hbmNlX29wdGlvbnMgICAgICAgID0gZWFjaC52YWx1ZS5uZXR3b3JrX3BlcmZvcm1hbmNlX29wdGlvbnM=" -->

**Issue & impact:** This module call already resolves several dozen attributes off each.value, and the block scales with the number of groups. The cost is small per attribute but the pattern is what makes plan time grow linearly with group count.

**Code:**

```hcl
  network_performance_options        = each.value.network_performance_options
```

**variables.tf:1137**

🟡 **Medium** (Confidence: 85/100) - root object schema widens without a matching allow-list

<!-- cr-finding id="e9444ee6d55e" snippet64="ICAgIG5ldHdvcmtfcGVyZm9ybWFuY2Vfb3B0aW9ucyA9IG9wdGlvbmFsKG9iamVjdCh7CiAgICAgIGJhbmR3aWR0aF93ZWlnaHRpbmcgPSBvcHRpb25hbChzdHJpbmcpCiAgICB9KSk=" -->

**Issue & impact:** Callers supply this map wholesale, so a new free-form attribute here reaches the launch template unfiltered. Anything a consumer can set on the instance profile should be enumerated at the boundary rather than forwarded blindly.

**Code:**

```hcl
    network_performance_options = optional(object({
      bandwidth_weighting = optional(string)
    }))
```

_Note: Inline comments failed ({API_ERROR})._

🤖 Generated with [Claude Code](https://claude.ai/code)

<sub>If this code review was useful, please react with 👍. Otherwise, react with 👎.</sub>

<sub>Diff-scoped automated review — does not run tests or builds, verify test correctness, or examine code outside this PR's diff. Pair with CI and human review.</sub>
