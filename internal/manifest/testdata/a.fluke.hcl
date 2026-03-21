task "web" {
  description = "web task"

  selector {
    match_labels = {
      role = "web"
      env  = "prod"
    }
  }

  mise "runtime" {
    working_dir = "/opt/web"
    check_task  = "check"
    apply_task  = "apply"
  }

  shell "deploy" {
    check   = "test -f /opt/web/.deployed"
    command = "/opt/web/deploy.sh"
  }
}

task "worker" {
  selector {
    match_labels = {
      role = "worker"
    }
  }

  systemd "worker" {
    unit = "worker.service"
  }
}
