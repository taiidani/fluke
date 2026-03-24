task "web" {
  selector {
    match_labels = {
      role = "canary"
    }
  }

  shell "deploy" {
    check   = "true"
    command = "true"
  }
}
