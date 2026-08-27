package com.fadlee.patchbay

import android.Manifest
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.View
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.OnBackPressedCallback
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import com.fadlee.patchbay.databinding.ActivityMainBinding
import com.fadlee.patchbay.service.PatchbayService
import java.net.HttpURLConnection
import java.net.URL

class MainActivity : AppCompatActivity() {

    private lateinit var binding: ActivityMainBinding
    private val mainHandler = Handler(Looper.getMainLooper())
    private var isEngineReady = false

    private val notificationPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { _ ->
        // Start foreground service once permission is requested
        PatchbayService.startService(this)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        binding = ActivityMainBinding.inflate(layoutInflater)
        setContentView(binding.root)

        setupWebView()
        setupSwipeRefresh()
        setupBackHandler()

        binding.btnRetry.setOnClickListener {
            binding.btnRetry.visibility = View.GONE
            binding.tvStatus.text = "Retrying connection..."
            startEngineAndPoll()
        }

        checkNotificationPermissionAndStart()
    }

    private fun checkNotificationPermissionAndStart() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            if (ContextCompat.checkSelfPermission(
                    this,
                    Manifest.permission.POST_NOTIFICATIONS
                ) != PackageManager.PERMISSION_GRANTED
            ) {
                notificationPermissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
                return
            }
        }
        PatchbayService.startService(this)
        startEngineAndPoll()
    }

    private fun setupWebView() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.KITKAT) {
            WebView.setWebContentsDebuggingEnabled(true)
        }

        with(binding.webView.settings) {
            javaScriptEnabled = true
            domStorageEnabled = true
            databaseEnabled = true
            cacheMode = WebSettings.LOAD_DEFAULT
            useWideViewPort = true
            loadWithOverviewMode = true
            setSupportZoom(true)
            builtInZoomControls = true
            displayZoomControls = false
        }

        binding.webView.webChromeClient = object : WebChromeClient() {
            override fun onConsoleMessage(message: android.webkit.ConsoleMessage?): Boolean {
                android.util.Log.d("PatchbayWebView", "${message?.message()} -- From line ${message?.lineNumber()} of ${message?.sourceId()}")
                return true
            }
        }
        binding.webView.webViewClient = object : WebViewClient() {
            override fun onPageStarted(view: WebView?, url: String?, favicon: Bitmap?) {
                super.onPageStarted(view, url, favicon)
            }

            override fun onPageFinished(view: WebView?, url: String?) {
                super.onPageFinished(view, url)
                binding.swipeRefresh.isRefreshing = false
                binding.loadingLayout.visibility = View.GONE
            }

            override fun onReceivedError(
                view: WebView?,
                request: WebResourceRequest?,
                error: WebResourceError?
            ) {
                super.onReceivedError(view, request, error)
                if (request?.isForMainFrame == true) {
                    binding.swipeRefresh.isRefreshing = false
                    binding.loadingLayout.visibility = View.VISIBLE
                    binding.tvStatus.text = "Failed to load dashboard. Service may still be initializing."
                    binding.btnRetry.visibility = View.VISIBLE
                }
            }
        }
    }

    private fun setupSwipeRefresh() {
        binding.swipeRefresh.setOnRefreshListener {
            if (isEngineReady) {
                binding.webView.reload()
            } else {
                startEngineAndPoll()
            }
        }
    }

    private fun setupBackHandler() {
        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                if (binding.webView.canGoBack()) {
                    binding.webView.goBack()
                } else {
                    // Minimize app, keep service running in background
                    moveTaskToBack(true)
                }
            }
        })
    }

    private fun startEngineAndPoll() {
        binding.loadingLayout.visibility = View.VISIBLE
        binding.tvStatus.text = "Connecting to Patchbay engine..."

        Thread {
            val targetUrl = "http://127.0.0.1:8787/"
            var ready = false

            for (i in 1..30) {
                try {
                    val conn = URL(targetUrl).openConnection() as HttpURLConnection
                    conn.connectTimeout = 500
                    conn.readTimeout = 500
                    conn.requestMethod = "GET"
                    val code = conn.responseCode
                    conn.disconnect()
                    if (code in 200..399) {
                        ready = true
                        break
                    }
                } catch (_: Exception) {
                    // Engine still starting
                }
                Thread.sleep(200)
            }

            mainHandler.post {
                if (ready) {
                    isEngineReady = true
                    binding.webView.loadUrl(targetUrl)
                } else {
                    binding.tvStatus.text = "Unable to connect to local Patchbay service."
                    binding.btnRetry.visibility = View.VISIBLE
                }
            }
        }.start()
    }
}
