export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        primary: "#00ADD8",
        "primary-dark": "#007D9C",
        "primary-light": "#5DC9E2",
        surface: "#FFFFFF",
        canvas: "#F8FAFC",
        ink: "#1E293B",
        muted: "#64748B",
        success: "#10B981",
        danger: "#EF4444",
      },
      borderRadius: {
        xl2: "1rem",
      },
      fontFamily: {
        sans: ["Inter", "system-ui", "Segoe UI", "sans-serif"],
      },
    },
  },
  plugins: [],
};
