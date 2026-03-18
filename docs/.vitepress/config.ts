import { defineConfig } from "vitepress";

export default defineConfig({
  title: "Fluke",
  description: "GitOps-based continuous delivery for VMs and bare metal",

  themeConfig: {
    nav: [
      { text: "Tutorials", link: "/tutorials/" },
      { text: "How-to Guides", link: "/how-to/" },
      { text: "Reference", link: "/reference/" },
      { text: "Explanation", link: "/explanation/" },
    ],

    sidebar: {
      "/tutorials/": [
        {
          text: "Tutorials",
          items: [
            { text: "Overview", link: "/tutorials/" },
            { text: "Getting Started", link: "/tutorials/getting-started" },
          ],
        },
      ],
      "/how-to/": [
        {
          text: "How-to Guides",
          items: [
            { text: "Overview", link: "/how-to/" },
            { text: "Configure TLS", link: "/how-to/configure-tls" },
            {
              text: "Use the mise Executor",
              link: "/how-to/use-the-mise-executor",
            },
            {
              text: "Use the Shell Executor",
              link: "/how-to/use-the-shell-executor",
            },
            {
              text: "Configure Drift Policy",
              link: "/how-to/configure-drift-policy",
            },
            {
              text: "Enable Redis Event Store",
              link: "/how-to/enable-redis-event-store",
            },
          ],
        },
      ],
      "/reference/": [
        {
          text: "Reference",
          items: [
            { text: "Overview", link: "/reference/" },
            { text: "Manifest", link: "/reference/manifest" },
            { text: "Configuration", link: "/reference/configuration" },
            { text: "Executors", link: "/reference/executors" },
            { text: "CLI", link: "/reference/cli" },
          ],
        },
      ],
      "/explanation/": [
        {
          text: "Explanation",
          items: [
            { text: "Overview", link: "/explanation/" },
            { text: "Architecture", link: "/explanation/architecture" },
            { text: "Executor Model", link: "/explanation/executor-model" },
            {
              text: "Drift & Reconciliation",
              link: "/explanation/drift-and-reconciliation",
            },
            { text: "Statelessness", link: "/explanation/statelessness" },
            { text: "Security Model", link: "/explanation/security-model" },
          ],
        },
      ],
    },

    socialLinks: [
      { icon: "github", link: "https://github.com/taiidani/fluke" },
    ],
  },
});
